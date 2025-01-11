package schedule

import (
	_ "embed"
	"encoding/json"
	"pkg/shelly/types"
)

//go:embed jobs.json
var raw []byte
var Jobs []JobSpec

func init() {
	err := json.Unmarshal(raw, &Jobs)
	if err != nil {
		panic(err)
	}
}

func ShowJobs(via types.Channel, d types.Device) (any, error) {
	out, err := d.CallE(via, "Schedule", "List", nil)
	if err != nil {
		log.Error(err, "Unable to list scheduled jobs")
		return nil, err
	}
	return out.(*Scheduled), nil
}

func ScheduleJobs(via types.Channel, d types.Device) (any, error) {
	for _, js := range Jobs {
		if !js.Enable {
			log.Info("Skipping disabled", "job", js)
			continue
		}
		log.Info("Scheduling", "job", js)
		_, err := scheduleOneJob(via, d, js)
		if err != nil {
			log.Error(err, "Unable to schedule", "job", js)
			return nil, err
		}
	}
	return ShowJobs(via, d)
}

func scheduleOneJob(via types.Channel, d types.Device, js JobSpec) (any, error) {
	out, err := d.CallE(via, "Schedule", "List", nil)
	if err != nil {
		log.Error(err, "Unable to list scheduled jobs")
		return nil, err
	}
	scheduled := out.(*Scheduled)
	// Look whether the job is already scheduled
	updated := false
	for _, job := range scheduled.Jobs {
		if job.Timespec == js.Timespec {
			// The job is already scheduled, update it
			log.Info("Updating scheduled", "job", job)
			_, err := d.CallE(via, "Schedule", "Update", &Job{JobId: job.JobId, JobSpec: js})
			if err != nil {
				log.Error(err, "Unable to update scheduled", "job_id", job.JobId)
				return nil, err
			}
			updated = true
		}
	}

	// The job is not scheduled yet, create it
	if !updated {
		log.Info("Scheduling", "job", js)
		return d.CallE(via, "Schedule", "Create", js)
	}
	return nil, nil
}

func CancelJob(via types.Channel, d types.Device, jobId uint32) (any, error) {
	out, err := d.CallE(via, "Schedule", "List", nil)
	if err != nil {
		log.Error(err, "Unable to list scheduled jobs")
		return nil, err
	}
	scheduled := out.(*Scheduled)
	// Look whether the job is already scheduled
	for _, job := range scheduled.Jobs {
		if job.JobId.Id == jobId {
			log.Info("Found scheduled", "job", job)
			_, err := d.CallE(via, "Schedule", "Delete", &JobId{Id: jobId})
			if err != nil {
				log.Error(err, "Unable to update scheduled", "job_id", job.JobId)
				return nil, err
			}
		}
	}

	// The job is not scheduled yet, create it
	log.Info("Cancelled", "jobId", jobId)
	return d.CallE(via, "Schedule", "List", nil)
}

func CancelAllJobs(via types.Channel, d types.Device) (any, error) {
	_, err := d.CallE(via, "Schedule", "DeleteAll", nil)
	if err != nil {
		log.Error(err, "Unable to cancel all scheduled jobs")
		return nil, err
	}
	log.Info("Cancelled all jobs")
	return d.CallE(via, "Schedule", "List", nil)
}
