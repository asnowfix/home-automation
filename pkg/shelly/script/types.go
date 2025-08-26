package script

// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Script

type ConfigurationRequest struct {
	Id            uint32        `json:"id"`     // Id of the script
	Configuration Configuration `json:"config"` // Configuration of the script
}

type Configuration struct {
	Id     uint32 `json:"id"`               // Id of the script
	Name   string `json:"name,omitempty"`   // Name of the script
	Enable bool   `json:"enable,omitempty"` // true if the script runs by default on boot, false otherwise
}

var Errors []string = []string{
	"crashed",          // The script caused a device crash
	"syntax_error",     // Incorrect javascript syntax
	"reference_error",  // Undefined variable
	"type_error",       // Accessing unexistent property or property with wrong type
	"out_of_memory",    // Out of memory
	"out_of_codespace", // Out of code space
	"internal_error",   // Internal interpreter error
	"not_implemented",  // Functionality not implemented
	"file_read_error",  // File read error
	"bad_arguments",    // Arguments fail verification
}

type Status struct {
	Id           uint32   `json:"id"`                  // Id of the script
	Running      bool     `json:"running"`             // true if the script is currently running, false otherwise (absent at configuration-time)
	Name         string   `json:"name,omitempty"`      // Name of the script
	MemUsed      uint32   `json:"mem_used,omitempty"`  // Memory used by the script in bytes
	MemPeak      uint32   `json:"mem_peak,omitempty"`  // Peak memory used by the script in bytes
	MemFree      uint32   `json:"mem_free,omitempty"`  // Free memory available to the script in bytes
	Manual       bool     `json:"loaded,omitempty"`    // Is loaded on the device
	Errors       []string `json:"errors,omitempty"`    // Optional, present only when the script execution resulted in an error. The array contains description of the type of error.
	ErrorMessage string   `json:"error_msg,omitempty"` // Optional, present only when the script execution resulted in an error. The array contains error message (stack trace).
}

type FormerStatus struct {
	WasRunning bool `json:"was_running"` // true if the script was running before the operation, false otherwise
}

type StatusStartStopDeleteRequest struct {
	Id uint32 `json:"id"` // Id of the script
}

type PutCodeRequest struct {
	Id     uint32 `json:"id"`               // Id of the script
	Code   string `json:"code"`             // The code which will be included in the script (the length must be greater than 0). Required
	Append bool   `json:"append,omitempty"` // true to append the code, false otherwise. If set to false, the existing code will be overwritten. Default value: false. Optional
}

type PutCodeResponse struct {
	Length uint `json:"len"` // The total code length in bytes
}

type GetCodeRequest struct {
	Id     uint32 `json:"id"`               // Id of the script
	Offset uint32 `json:"offset,omitempty"` // Byte offset from the beginning. Default value: 0. Optional
	Length uint32 `json:"len,omitempty"`    // Bytes to read. Default value: maximum possible number of bytes till the end is reached. Optional
}

type GetCodeResponse struct {
	Data string `json:"data"` // The requested data chunk
	Left uint32 `json:"left"` // Number of bytes remaining till the end of the code
}

type EvalRequest struct {
	Id   uint32 `json:"id"`   // Id of the script
	Code string `json:"code"` // Argument to evaluate (the length must be greater than 0). Required
}

type EvalResponse struct {
	Result string `json:"result"` // The result of the evaluation
}

type ListResponse struct {
	Scripts []Status `json:"scripts"` // List of scripts
}
