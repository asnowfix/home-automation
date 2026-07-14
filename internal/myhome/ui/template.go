package ui

import (
	"context"
	"embed"
	"io"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"text/template"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/myhome/storage"
	"github.com/asnowfix/home-automation/pkg/shelly"
	pkgshelly "github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"

	"github.com/asnowfix/home-automation/internal/global"

	"github.com/go-logr/logr"
)

// Embed static assets under this package
//
//go:embed static/*
var staticFS embed.FS

// StaticFS returns the embedded static filesystem
func StaticFS() (fs.FS, error) {
	return fs.Sub(staticFS, "static")
}

// StaticFileServer returns an http.Handler for serving static files
func StaticFileServer() (http.Handler, error) {
	sub, err := StaticFS()
	if err != nil {
		return nil, err
	}
	return http.FileServer(http.FS(sub)), nil
}

// DeviceView represents a device for rendering in the UI
type DeviceView struct {
	Name                 string                          `json:"name"`
	Id                   string                          `json:"id"`
	Manufacturer         string                          `json:"manufacturer"`
	Host                 string                          `json:"host"`
	LinkToken            string                          `json:"link_token"`
	HasWebUI             bool                            `json:"has_web_ui"` // true if the device exposes an HTTP web UI at all (excludes BLU); reachability itself is resolved live when the link is opened, see internal/myhome/proxy
	IsRefreshable        bool                            `json:"is_refreshable"`
	HasHeaterScript      bool                            `json:"has_heater_script"`
	HasDoorSensor        bool                            `json:"has_door_sensor"`               // true if device has door/window sensing capability
	HasTemperatureSensor bool                            `json:"has_temperature_sensor"`        // true if device has temperature sensing capability
	HasHumiditySensor    bool                            `json:"has_humidity_sensor"`           // true if device has humidity sensing capability
	DeviceTypeEmoji      string                          `json:"device_type_emoji"`             // Emoji indicating device type (e.g., 🌡️ for thermometer, 🚶 for motion)
	Temperature          *float64                        `json:"temperature,omitempty"`         // Current temperature in Celsius (nil if not a thermometer)
	Humidity             *float64                        `json:"humidity,omitempty"`            // Current humidity in percentage (nil if not a humidity sensor)
	DoorOpened           *bool                           `json:"door_opened,omitempty"`         // true if door/window is open, false if closed (nil if not a door/window sensor)
	Switches             map[int]pkgshelly.SwitchSummary `json:"switches,omitempty"`            // Switches on the device (nil if not a switch)
	IsPoolPump           bool                            `json:"is_pool_pump,omitempty"`        // true if this is the configured pool pump device
	TurnoverAchieved     *float64                        `json:"turnover_achieved,omitempty"`   // pool volumes filtered today so far (nil unless IsPoolPump)
	TurnoverTarget       *float64                        `json:"turnover_target,omitempty"`     // configured daily turnover target, times/day (nil unless IsPoolPump)
	WaterSupplyActive    *bool                           `json:"water_supply_active,omitempty"` // true = water-supply protection engaged, pump paused (nil unless IsPoolPump)
}

// applyPoolStatus enriches views in place with the configured pool device's
// turnover rate and water-supply status, fetched once via the pool.getstatus
// RPC method (myhome/daemon/pool_rpc.go) rather than once per device — there
// is exactly one configured pool device system-wide, so doing this inside
// DeviceToView itself would trigger a redundant KVS/MQTT round trip for
// every other device on each dashboard refresh.
func applyPoolStatus(ctx context.Context, views []DeviceView) {
	mh, err := myhome.Methods(myhome.PoolGetStatus)
	if err != nil {
		return // pool RPC not registered (pool tracking disabled)
	}
	res, err := mh.ActionE(ctx, nil)
	if err != nil {
		return // device unreachable / KVS misconfigured — leave fields blank
	}
	status, ok := res.(*myhome.PoolGetStatusResult)
	if !ok {
		return
	}
	for i := range views {
		if views[i].Id != status.DeviceID {
			continue
		}
		achieved, target, active := status.TurnoverAchieved, status.TurnoverTarget, status.WaterSupplyActive
		views[i].IsPoolPump = true
		views[i].TurnoverAchieved = &achieved
		views[i].TurnoverTarget = &target
		views[i].WaterSupplyActive = &active
	}
}

// IndexData holds the data for rendering the index page
type IndexData struct {
	Version string
	Devices []DeviceView
}

// index template and renderer
var indexTmpl *template.Template

// eventLogTmpl is the full-page event log template
var eventLogTmpl *template.Template

func init() {
	indexContent, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		panic(err)
	}

	// Create template with custom functions
	tmpl := template.New("index").Funcs(template.FuncMap{
		"lower": strings.ToLower,
	})
	indexTmpl = template.Must(tmpl.Parse(string(indexContent)))

	// Build the event-log page template
	eventLogTmpl = template.Must(template.New("event-log").Funcs(template.FuncMap{
		"lower": strings.ToLower,
	}).Parse(eventLogPageHTML))
}

// DeviceToView converts a myhome.Device to ui.DeviceView for SSE broadcasting and UI rendering
// This is the canonical conversion function used by both initial page rendering and SSE updates
func DeviceToView(ctx context.Context, d *myhome.Device) DeviceView {
	log, err := logr.FromContext(ctx)
	if err != nil {
		panic("BUG: No logger initialized")
	}

	name := d.Name()
	if name == "" {
		name = d.Id()
	}
	host := d.Host()
	token := host
	if token == "" {
		token = d.Name()
		if token == "" {
			token = d.Id()
		}
	}

	// Check for heater script
	hasHeater := false
	if d.Config != nil {
		for _, s := range []*pkgshelly.ScriptInfo{d.Config.Script1, d.Config.Script2, d.Config.Script3, d.Config.Script4} {
			if s != nil && s.Name == "heater.js" {
				hasHeater = true
				break
			}
		}
	}

	// Check for temperature capability
	hasTemp := strings.HasPrefix(d.Id(), "shellyht-")
	if !hasTemp && d.Info != nil && d.Info.BTHome != nil {
		for _, cap := range d.Info.BTHome.Capabilities {
			if cap == "temperature" {
				hasTemp = true
				break
			}
		}
	}

	hasHumidity := strings.HasPrefix(d.Id(), "shellyht-")
	if !hasHumidity && d.Info != nil && d.Info.BTHome != nil {
		for _, cap := range d.Info.BTHome.Capabilities {
			if cap == "humidity" {
				hasHumidity = true
				break
			}
		}
	}

	// Check for window/door capability
	hasDoor := false
	if d.Info != nil && d.Info.BTHome != nil {
		for _, cap := range d.Info.BTHome.Capabilities {
			if cap == "window" {
				hasDoor = true
				break
			}
		}
	}

	// Get device type emoji
	emoji := ""
	if strings.HasPrefix(d.Id(), "shellyht-") {
		emoji = "🌡️"
	} else if d.Info != nil && d.Info.BTHome != nil {
		caps := d.Info.BTHome.Capabilities
		hasMotion := false
		hasButton := false
		for _, cap := range caps {
			switch cap {
			case "motion":
				hasMotion = true
			case "button":
				hasButton = true
			}
		}
		// Priority: motion > window > button > temperature
		if hasMotion {
			emoji = "🚶"
		} else if hasDoor {
			emoji = "🚪"
		} else if hasButton {
			emoji = "🔘"
		} else if hasTemp {
			emoji = "🌡️"
		}
	}

	// Check if device is refreshable
	isRefreshable := !shelly.IsBluDevice(d.Id()) && !shelly.IsGen1Device(d.Id())

	// BLU devices are BLE-only sensors with no HTTP/web interface at all.
	// Everything else has one, whether or not we currently have a cached
	// dialable address for it — Host is no longer persisted (see #252), and
	// the /devices/{token}/ proxy route resolves the device live (via MAC,
	// then mDNS on its device ID) at click time instead.
	hasWebUI := !shelly.IsBluDevice(d.Id())

	// Get switch information
	switches := make(map[int]pkgshelly.SwitchSummary)

	// First try to get switches from config if available
	if d.Config != nil {
		log.V(1).Info("Device has config", "device", d.Id(), "switch0", d.Config.Switch0 != nil, "switch1", d.Config.Switch1 != nil)

		// Get device implementation to access status
		var status *pkgshelly.Status
		if sd, ok := d.Impl().(*shelly.Device); ok && sd != nil {
			status = sd.Status()
		}

		for _, sw := range []*sswitch.Config{d.Config.Switch0, d.Config.Switch1, d.Config.Switch2, d.Config.Switch3} {
			if sw != nil {
				isOn := false
				// Get actual switch status if available
				if status != nil {
					switch sw.Id {
					case 0:
						if status.Switch0 != nil {
							isOn = status.Switch0.Output
						}
					case 1:
						if status.Switch1 != nil {
							isOn = status.Switch1.Output
						}
					case 2:
						if status.Switch2 != nil {
							isOn = status.Switch2.Output
						}
					case 3:
						if status.Switch3 != nil {
							isOn = status.Switch3.Output
						}
					}
				}

				switches[sw.Id] = pkgshelly.SwitchSummary{
					Id:   sw.Id,
					Name: sw.Name,
					On:   isOn,
				}
			}
		}
		log.V(1).Info("Loaded switches from config", "device", d.Id(), "count", len(switches))
	} else if sd, ok := d.Impl().(*shelly.Device); ok && sd != nil {
		// Fallback: Fetch switches directly from device if no config
		log.V(1).Info("No config, fetching switches from device", "device", d.Id())
		switchSummary, err := pkgshelly.GetSwitchesSummary(ctx, sd)
		if err != nil {
			log.V(1).Info("Failed to get switches summary", "device", d.Id(), "error", err)
		} else {
			switches = switchSummary
			log.V(1).Info("Fetched switches from device", "device", d.Id(), "count", len(switches))
		}
	}

	// Extract sensor values from cached device status
	var temperature *float64
	var humidity *float64
	var doorOpened *bool

	if d.Status != nil && d.Status.Sensors != nil {
		temperature = d.Status.Sensors.Temperature
		humidity = d.Status.Sensors.Humidity

		// Convert Window sensor (0=closed, 1=open) to DoorOpened bool
		if d.Status.Sensors.Window != nil {
			opened := *d.Status.Sensors.Window == 1
			doorOpened = &opened
		}

		// Debug logging to trace sensor values
		log.V(1).Info("DeviceToView sensor values",
			"device_id", d.Id(),
			"temperature", temperature,
			"humidity", humidity,
			"window", d.Status.Sensors.Window,
			"door_opened", doorOpened)
	} else {
		log.V(1).Info("DeviceToView no sensor data",
			"device_id", d.Id(),
			"has_status", d.Status != nil,
			"has_sensors", d.Status != nil && d.Status.Sensors != nil)
	}

	return DeviceView{
		Id:                   d.Id(),
		Name:                 name,
		Manufacturer:         d.Manufacturer(),
		Host:                 host,
		LinkToken:            token,
		HasWebUI:             hasWebUI,
		IsRefreshable:        isRefreshable,
		HasHeaterScript:      hasHeater,
		HasDoorSensor:        hasDoor,
		HasTemperatureSensor: hasTemp,
		HasHumiditySensor:    hasHumidity,
		DeviceTypeEmoji:      emoji,
		Temperature:          temperature,
		Humidity:             humidity,
		DoorOpened:           doorOpened,
		Switches:             switches,
	}
}

// RenderEventLog renders the event log page.
// It reuses the same index layout but injects the events table section.
func RenderEventLog(ctx context.Context, db *storage.DeviceStorage, w io.Writer) error {
	data := IndexData{
		Devices: []DeviceView{},
		Version: ctx.Value(global.VersionKey).(string),
	}
	if db != nil {
		devices, err := db.GetAllDevices(ctx)
		if err == nil {
			for _, d := range devices {
				data.Devices = append(data.Devices, DeviceToView(ctx, d))
			}
		}
	}
	return eventLogTmpl.Execute(w, data)
}

// RenderIndex renders the index page with device list
// Sensor values are read from cache and updated via SSE when they change
func RenderIndex(ctx context.Context, db *storage.DeviceStorage, w io.Writer) error {
	data := IndexData{
		Devices: []DeviceView{},
		Version: ctx.Value(global.VersionKey).(string),
	}
	if db != nil {
		devices, err := db.GetAllDevices(ctx)
		if err != nil {
			return indexTmpl.Execute(w, data)
		}
		for _, d := range devices {
			data.Devices = append(data.Devices, DeviceToView(ctx, d))
		}
		sort.Slice(data.Devices, func(i, j int) bool {
			return strings.ToLower(data.Devices[i].Name) < strings.ToLower(data.Devices[j].Name)
		})
		applyPoolStatus(ctx, data.Devices)
	}
	return indexTmpl.Execute(w, data)
}
