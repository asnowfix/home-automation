package schedule

// https://shelly-api-docs.shelly.cloud/gen2/0.14/ComponentsAndServices/Schedule

type JobsRevision struct {
	Revision uint `json:"rev"`
}

type JobId struct {
	// Id assigned to the job when it is created. This is used in subsequent Update / Delete calls
	Id uint32 `json:"id,omitempty"`
}

type JobCall struct {
	// Name of the RPC method. Required
	Method string `json:"method"`
	// Parameters to be passed to the RPC method. Optional
	Params interface{} `json:"params,omitempty"`
}

type JobSpec struct {
	Enable   bool      `json:"enable"`   // true to enable the execution of this job, false otherwise. It is true by default.
	Timespec string    `json:"timespec"` // As defined by [mongoose cron](https://github.com/mongoose-os-libs/cron). Note that leading 0s are not supported (e.g.: for 8 AM you should set 8 instead of 08).
	Calls    []JobCall `json:"calls"`    // RPC methods and arguments to be invoked when the job gets executed. It must contain at least one valid object. There is a limit of 5 calls per schedule job
}

type Job struct {
	JobId
	JobSpec
}

type Scheduled struct {
	Jobs []Job `json:"jobs"`
}
