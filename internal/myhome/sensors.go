package myhome

// Sensors represents all possible BTHome v2 sensor readings
// Specification: https://bthome.io/format/
// All fields are pointers to distinguish between "not present" (nil) and "zero value"
type Sensors struct {
	// Power & Energy (0x01, 0x0a, 0x0b, 0x0c, 0x43)
	Battery *int     `json:"battery,omitempty"` // 0x01 - Battery level in %
	Energy  *float64 `json:"energy,omitempty"`  // 0x0a - Energy in kWh
	Power   *float64 `json:"power,omitempty"`   // 0x0b - Power in W
	Voltage *float64 `json:"voltage,omitempty"` // 0x0c - Voltage in V
	Current *float64 `json:"current,omitempty"` // 0x43 - Current in A

	// Environmental Sensors (0x02, 0x03, 0x04, 0x05, 0x06, 0x08, 0x45)
	Temperature *float64 `json:"temperature,omitempty"` // 0x02/0x45 - Temperature in °C
	Humidity    *float64 `json:"humidity,omitempty"`    // 0x03/0x2e - Humidity in %
	Pressure    *float64 `json:"pressure,omitempty"`    // 0x04 - Pressure in hPa
	Illuminance *float64 `json:"illuminance,omitempty"` // 0x05 - Illuminance in lux
	Mass        *float64 `json:"mass,omitempty"`        // 0x06 - Mass in kg
	DewPoint    *float64 `json:"dew_point,omitempty"`   // 0x08 - Dew point in °C

	// Motion & Position (0x21, 0x2d, 0x3a, 0x3f)
	Motion   *int     `json:"motion,omitempty"`   // 0x21 - Motion (0=clear, 1=detected)
	Window   *int     `json:"window,omitempty"`   // 0x2d - Window/Door (0=closed, 1=open)
	Button   *int     `json:"button,omitempty"`   // 0x3a - Button press count
	Rotation *float64 `json:"rotation,omitempty"` // 0x3f - Rotation in degrees

	// Distance (0x40, 0x41)
	DistanceMM *int     `json:"distance_mm,omitempty"` // 0x40 - Distance in mm
	DistanceM  *float64 `json:"distance_m,omitempty"`  // 0x41 - Distance in m

	// Other (0x50, 0x51)
	Timestamp    *int     `json:"timestamp,omitempty"`    // 0x50 - Unix timestamp in seconds
	Acceleration *float64 `json:"acceleration,omitempty"` // 0x51 - Acceleration in m/s²
}
