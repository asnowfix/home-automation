package schedule

import (
	"context"
	"errors"
	"testing"

	"github.com/asnowfix/home-automation/pkg/shelly/types"
	"github.com/go-logr/logr"
)

func TestShowJobs(t *testing.T) {
	t.Run("happy path returns Scheduled", func(t *testing.T) {
		d := types.NewFakeDevice()
		want := &Scheduled{Jobs: []Job{{JobId: JobId{Id: 1}}}}
		d.SetResult(string(List), want)

		got, err := ShowJobs(context.Background(), logr.Discard(), types.ChannelDefault, d)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.(*Scheduled) != want {
			t.Errorf("expected %+v, got %+v", want, got)
		}
		if len(d.Calls) != 1 || d.Calls[0].Method != string(List) {
			t.Errorf("expected a single Schedule.List call, got %+v", d.Calls)
		}
	})

	t.Run("device error propagates", func(t *testing.T) {
		d := types.NewFakeDevice()
		wantErr := errors.New("device unreachable")
		d.SetError(string(List), wantErr)

		_, err := ShowJobs(context.Background(), logr.Discard(), types.ChannelDefault, d)
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected %v, got %v", wantErr, err)
		}
	})
}

func TestCancelJob(t *testing.T) {
	d := types.NewFakeDevice()
	d.SetResult(string(List), &Scheduled{Jobs: []Job{{JobId: JobId{Id: 42}}}})
	d.SetResult(string(Delete), &JobId{Id: 42})

	_, err := CancelJob(context.Background(), logr.Discard(), types.ChannelDefault, d, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var deletedId uint32
	found := false
	for _, c := range d.Calls {
		if c.Method == string(Delete) {
			deletedId = c.Params.(*JobId).Id
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a Schedule.Delete call, got %+v", d.Calls)
	}
	if deletedId != 42 {
		t.Errorf("expected job id 42 deleted, got %d", deletedId)
	}
}

func TestCancelJob_NoMatchingJob_NoDeleteCall(t *testing.T) {
	d := types.NewFakeDevice()
	d.SetResult(string(List), &Scheduled{Jobs: []Job{{JobId: JobId{Id: 99}}}})

	_, err := CancelJob(context.Background(), logr.Discard(), types.ChannelDefault, d, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range d.Calls {
		if c.Method == string(Delete) {
			t.Fatalf("expected no Schedule.Delete call for a non-matching job id, got %+v", d.Calls)
		}
	}
}

func TestCancelAllJobs(t *testing.T) {
	d := types.NewFakeDevice()
	d.SetResult(string(DeleteAll), nil)
	d.SetResult(string(List), &Scheduled{})

	_, err := CancelAllJobs(context.Background(), logr.Discard(), types.ChannelDefault, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(d.Calls) != 2 || d.Calls[0].Method != string(DeleteAll) || d.Calls[1].Method != string(List) {
		t.Errorf("expected DeleteAll then List, got %+v", d.Calls)
	}
}

func TestCancelAllJobs_DeleteAllError(t *testing.T) {
	d := types.NewFakeDevice()
	wantErr := errors.New("delete-all failed")
	d.SetError(string(DeleteAll), wantErr)

	_, err := CancelAllJobs(context.Background(), logr.Discard(), types.ChannelDefault, d)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}
