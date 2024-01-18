// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Input/
package input

type Type uint

const (
	Switch Type = iota
	Button
	Analog
	Count
)

func (t Type) String() string {
	return [...]string{"switch", "button", "analog", "count"}[t]
}

// The configuration of the Input component contains information about the type, invert, and factory reset settings of the chosen input instance. To Get/Set the configuration of the Input component its id must be specified.
// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Input#configuration
type Configuration struct {
	// Id of the Input component instance
	Id string `json:"id"`
	// Name of the input instance
	Name string `json:"name"`
	// Type of associated input. Range of values switch, button, analog, count (only if applicable).
	Type string `json:"type"`
	// Global enable flag. When disabled, the input instance doesn't emit any events and reports status properties as null. Applies for all input types
	Enable bool `json:"enable"`
	// (only for type switch, button, analog) True if the logical state of the associated input is inverted, false otherwise. For the change to be applied, the physical switch has to be toggled once after invert is set. For type analog inverts percent range - 100% becomes 0% and 0% becomes 100%
	Invert bool `json:"invert,omitempty"`
	// (only for type switch, button) True if input-triggered factory reset option is enabled, false otherwise (shown if applicable)
	FactoryReset bool `json:"factory_reset,omitempty"`
	// (only for type analog) Analog input report threshold in percent. The accepted range is device-specific, default [1.0..50.0]% unless specified otherwise
	ReportThreshold float32 `json:"report_thr"`
	// (only for type analog) Remaps 0%-100% range to values in array. The first value in the array is the min setting, and the second value is the max setting. Array elements are of type number. Float values are supported. The accepted range for values is from 0% to 100%. Default values are [0, 100]. max must be greater than min. Equality is supported.
	RangeMap []float32 `json:"range_map,omitempty"`
	// (only for type analog) Analog input range, which is device-specific. See the table below.
	Range float32 `json:"range,omitempty"`
	// (only for type analog) Value transformation config for status.percent
	Xpercent struct {
		// JS expression containing x, where x is the raw value to be transformed (status.percent), for example "x+1". Accepted range: null or [0..100] chars. Both null and "" mean value transformation is disabled.
		Expression string `json:"expr,omitempty"`
		// Unit of the transformed value (status.xpercent), for example, "m/s". Accepted range: null or [0..20] chars. Both null and "" mean value transformation is disabled.
		Unit string `json:"unit,omitempty"`
	} `json:"xpercent,omitempty"`
	// TBC
}

// The status of the Input component contains information about the state of the chosen input instance. To obtain the status of the Input component its id must be specified.
// https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Input#status
type Status struct {
	// Id of the Input component instance
	Id string `json:"id"`
	// (only for type switch, button) State of the input (null if the input instance is stateless, i.e. for type button)
	State bool `json:"state,omitempty"`
	// (only for type analog) Analog value in percent (null if the valid value could not be obtained)
	Percent float32 `json:"percent,omitempty"`
	// (only for type count) Information about the counted pulses.
	Counts struct {
		// Total pulses counted.
		Total uint32 `json:"total"`
		// total transformed with config.xcounts.expr. Present only when both config.xcounts.expr and config.xcounts.unit are set to non-empty values. null if config.xcounts.expr can not be evaluated.
		XTotal uint32 `json:"xtotal,omitempty"`
		// Counted pulses per minute for the last three minutes (the lower the index of the element in the array, the closer to the current moment the minute) Present only if the device clock is synced.
		ByMinute []uint32 `json:"by_minute"`
		// by_minute transformed with config.xcounts.expr. Present only when both config.xcounts.expr and config.xcounts.unit is set to non-empty values and the device clock is synced. null if config.xcounts.expr can not be evaluated.
		XByMinute uint32 `json:"xby_minute,omitempty"`
		// Unix timestamp of the first second of the last minute (in UTC)
		MinuteTimestamp uint32 `json:"minute_ts"`
	}
	// (only for type count) Measured frequency in Hz. Determined at every elapsed freq_window period.
	Frequency float32 `json:"freq,omitempty"`
	// (only for type count) freq transformed with config.xfreq.expr. Present only when both config.xfreq.expr and config.xfreq.unit are set to non-empty values. null if config.xfreq.expr can not be evaluated.
	XFrequency float32 `json:"xfreq,omitempty"`
	// Shown only if at least one error is present. May contain out_of_range, read
	Errors []string `json:"errors"`
}
