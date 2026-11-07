package cloud

// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Cloud#cloudgetstatus

// Status is the response from Cloud.GetStatus.
type Status struct {
	Connected          bool   `json:"connected"`
	WebsocketConnected bool   `json:"websocket_connected"`
	DisconnectedReason string `json:"disconnected_reason,omitempty"`
}
