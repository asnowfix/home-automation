package script

// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Script

type Configuration struct {
	Id     uint32 `json:"id"`
	Name   string `json:"name"`
	Enable bool   `json:"enable"`
}

type Status struct {
	Id      uint32   `json:"id"`
	Running bool     `json:"error"`
	Error   []string `json:"errors"`
}
