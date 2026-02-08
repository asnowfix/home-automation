package ui

import (
	"context"
	"embed"
	"io"
	"io/fs"
	"myhome"
	"myhome/storage"
	"net/http"
	"sort"
	"strings"
	"text/template"
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
	Name                 string   `json:"name"`
	Id                   string   `json:"id"`
	Manufacturer         string   `json:"manufacturer"`
	Host                 string   `json:"host"`
	LinkToken            string   `json:"link_token"`
	HasHeaterScript      bool     `json:"has_heater_script"`
	HasDoorSensor        bool     `json:"has_door_sensor"`        // true if device has door/window sensing capability
	HasTemperatureSensor bool     `json:"has_temperature_sensor"` // true if device has temperature sensing capability
	HasHumiditySensor    bool     `json:"has_humidity_sensor"`    // true if device has humidity sensing capability
	DeviceTypeEmoji      string   `json:"device_type_emoji"`      // Emoji indicating device type (e.g., 🌡️ for thermometer, 🚶 for motion)
	Temperature          *float64 `json:"temperature,omitempty"`  // Current temperature in Celsius (nil if not a thermometer)
	Humidity             *float64 `json:"humidity,omitempty"`     // Current humidity in percentage (nil if not a humidity sensor)
	DoorOpened           *bool    `json:"door_opened,omitempty"`  // true if door/window is open, false if closed (nil if not a door/window sensor)
}

// IndexData holds the data for rendering the index page
type IndexData struct {
	Devices []DeviceView
}

// index template and renderer
var indexTmpl *template.Template

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
}

// DeviceToView converts a myhome.Device to ui.DeviceView for SSE broadcasting and UI rendering
// This is the canonical conversion function used by both initial page rendering and SSE updates
func DeviceToView(d *myhome.Device) DeviceView {
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
		for _, s := range d.Config.Scripts {
			if s.Name == "heater.js" {
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

	return DeviceView{
		Id:                   d.Id(),
		Name:                 name,
		Manufacturer:         d.Manufacturer(),
		Host:                 host,
		LinkToken:            token,
		HasHeaterScript:      hasHeater,
		HasDoorSensor:        hasDoor,
		HasTemperatureSensor: hasTemp,
		HasHumiditySensor:    hasHumidity,
		DeviceTypeEmoji:      emoji,
		Temperature:          nil, // Sensor values are updated separately via SSE
		DoorOpened:           nil,
	}
}

// RenderIndex renders the index page with device list
// Sensor values are populated via SSE after page load
func RenderIndex(ctx context.Context, db *storage.DeviceStorage, w io.Writer) error {
	data := IndexData{Devices: []DeviceView{}}
	if db != nil {
		devices, err := db.GetAllDevices(ctx)
		if err != nil {
			return indexTmpl.Execute(w, data)
		}
		for _, d := range devices {
			data.Devices = append(data.Devices, DeviceToView(d))
		}
		sort.Slice(data.Devices, func(i, j int) bool {
			return strings.ToLower(data.Devices[i].Name) < strings.ToLower(data.Devices[j].Name)
		})
	}
	return indexTmpl.Execute(w, data)
}
