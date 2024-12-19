package schedule

import (
	"devices/shelly/types"
	"net/http"
	"reflect"

	"github.com/go-logr/logr"
)

var log logr.Logger

type empty struct{}

func Init(l logr.Logger, r types.MethodsRegistrar) {
	log = l
	log.Info("Init package", reflect.TypeOf(empty{}).PkgPath())
	r.RegisterMethodHandler("Schedule", "Create", types.MethodHandler{
		Allocate:   func() any { return new(Job) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler("Schedule", "Update", types.MethodHandler{
		Allocate:   func() any { return new(Job) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Schedule", "List", types.MethodHandler{
		Allocate:   func() any { return new(Scheduled) },
		HttpMethod: http.MethodGet,
	})
	r.RegisterMethodHandler("Schedule", "Delete", types.MethodHandler{
		Allocate:   func() any { return new(JobId) },
		HttpMethod: http.MethodPost,
	})
	r.RegisterMethodHandler("Schedule", "DeleteAll", types.MethodHandler{
		Allocate:   func() any { return nil },
		HttpMethod: http.MethodPost,
	})
}

func ShowJobs(via types.Channel, d types.Device) (any, error) {
	return d.CallE(via, "Schedule", "List", nil)
}

func ScheduleJob(via types.Channel, d types.Device, js JobSpec) (any, error) {
	out, err := d.CallE(via, "Schedule", "List", nil)
	if err != nil {
		log.Error(err, "Unable to list scheduled jobs")
		return nil, err
	}
	scheduled := out.(*Scheduled)
	// Look whether the job is already scheduled
	for _, job := range scheduled.Jobs {
		if job.Timespec == js.Timespec {
			// The job is already scheduled, update it
			log.Info("Updating scheduled", "job", job)
			_, err := d.CallE(via, "Schedule", "Update", &Job{JobId: job.JobId, JobSpec: js})
			if err != nil {
				log.Error(err, "Unable to update scheduled", "job_id", job.JobId)
				return nil, err
			}

			out, err = d.CallE(via, "Schedule", "List", &Job{JobId: job.JobId, JobSpec: js})
			if err != nil {
				log.Error(err, "Unable to list scheduled job after update")
				return nil, err
			}

			for _, uj := range out.(*Scheduled).Jobs {
				if job.JobId == uj.JobId {
					return &uj, nil
				}
			}
		}
	}

	// The job is not scheduled yet, create it
	log.Info("Scheduling", "job", js)
	return d.CallE(via, "Schedule", "Create", js)
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
