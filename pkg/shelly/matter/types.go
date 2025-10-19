package matter

// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Matter

// Config represents the Matter component configuration
type Config struct {
	Enable bool `json:"enable"` // Set true to enable Matter server
}

// Status represents the Matter component status
type Status struct {
	NumFabrics     int  `json:"num_fabrics"`    // The number of Matter fabrics that the device has joined
	Commissionable bool `json:"commissionable"` // true if the device can be joined to an existing Matter fabric
}

// SetupCode represents the response from Matter.GetSetupCode
type SetupCode struct {
	QrCode     string `json:"qr_code"`     // Textual representation of the QR code used to join the device
	ManualCode string `json:"manual_code"` // Manual pairing code for commissioning
}

// SetConfigRequest represents the request for Matter.SetConfig
type SetConfigRequest struct {
	Config Config `json:"config"`
}
