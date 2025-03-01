package temperature

// The status of the Temperature component represents the measurement of the associated temperature sensor. To obtain the status of the Temperature component its id must be specified.
type Status struct {
	// Id of the Temperature component instance
	Id uint32 `json:"id"`
	// Temperature in Celsius (null if valid value could not be obtained)
	Celsius float32 `json:"tC,omitempty"`
	// Temperature in Fahrenheit (null if valid value could not be obtained)
	Fahrenheit float32 `json:"tF,omitempty"`
	// Shown only if at least one error is present. May contain out_of_range, read when there is problem reading sensor
	Errors []string `json:"errors,omitempty"`
}
