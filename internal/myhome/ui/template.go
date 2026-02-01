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
	Name                 string
	Id                   string
	Manufacturer         string
	Host                 string
	LinkToken            string
	HasHeaterScript      bool
	HasDoorSensor        bool     // true if device has door/window sensing capability
	HasTemperatureSensor bool     // true if device has temperature sensing capability
	DeviceTypeEmoji      string   // Emoji indicating device type (e.g., 🌡️ for thermometer, 🚶 for motion)
	Temperature          *float64 // Current temperature in Celsius (nil if not a thermometer)
	DoorOpened           *bool    // true if door/window is open, false if closed (nil if not a door/window sensor)
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
	indexTmpl = template.Must(template.New("index").Parse(string(indexContent)))
}

// hasHeaterScript checks if a device has the heater.js script installed
func hasHeaterScript(d *myhome.Device) bool {
	if d.Config == nil {
		return false
	}
	for _, s := range d.Config.Scripts {
		if s.Name == "heater.js" {
			return true
		}
	}
	return false
}

// getDeviceTypeEmoji returns an emoji indicating the device type based on capabilities
func getDeviceTypeEmoji(d *myhome.Device) string {
	// Check for Gen1 H&T devices by ID pattern
	if strings.HasPrefix(d.Id(), "shellyht-") {
		return "🌡️"
	}

	// Check BLU device capabilities
	if d.Info == nil || d.Info.BTHome == nil {
		return ""
	}

	caps := d.Info.BTHome.Capabilities
	if len(caps) == 0 {
		return ""
	}

	// Check for specific capabilities and return appropriate emoji
	hasMotion := false
	hasTemperature := false
	hasButton := false
	hasWindow := false

	for _, cap := range caps {
		switch cap {
		case "motion":
			hasMotion = true
		case "temperature":
			hasTemperature = true
		case "button":
			hasButton = true
		case "window":
			hasWindow = true
		}
	}

	// Priority: motion > window > button > temperature
	if hasMotion {
		return "🚶"
	}
	if hasWindow {
		return "🚪"
	}
	if hasButton {
		return "🔘"
	}
	if hasTemperature {
		return "🌡️"
	}

	return ""
}

// hasTemperatureCapability checks if a device has temperature sensing capability
func hasTemperatureCapability(d *myhome.Device) bool {
	if strings.HasPrefix(d.Id(), "shellyht-") {
		return true
	}
	if d.Info != nil && d.Info.BTHome != nil {
		for _, cap := range d.Info.BTHome.Capabilities {
			if cap == "temperature" {
				return true
			}
		}
	}
	return false
}

// hasWindowCapability checks if a device has window/door sensing capability
func hasWindowCapability(d *myhome.Device) bool {
	if d.Info != nil && d.Info.BTHome != nil {
		for _, cap := range d.Info.BTHome.Capabilities {
			if cap == "window" {
				return true
			}
		}
	}
	return false
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
			view := DeviceView{
				Name:                 name,
				Id:                   d.Id(),
				Manufacturer:         d.Manufacturer(),
				Host:                 host,
				LinkToken:            token,
				HasHeaterScript:      hasHeaterScript(d),
				HasDoorSensor:        hasWindowCapability(d),
				HasTemperatureSensor: hasTemperatureCapability(d),
				DeviceTypeEmoji:      getDeviceTypeEmoji(d),
			}

			data.Devices = append(data.Devices, view)
		}
		sort.Slice(data.Devices, func(i, j int) bool {
			return strings.ToLower(data.Devices[i].Name) < strings.ToLower(data.Devices[j].Name)
		})
	}
	return indexTmpl.Execute(w, data)
}
