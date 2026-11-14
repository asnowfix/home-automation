package shelly

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/asnowfix/home-automation/pkg/shelly/shelly"
	"github.com/asnowfix/home-automation/pkg/shelly/sswitch"
	"github.com/go-logr/logr"
)

func newStatusDevice(t *testing.T, st *shelly.Status) *Device {
	t.Helper()
	d := &Device{log: logr.Discard()}
	d.SetStatus(st)
	return d
}

func TestApplyStatusNotification_SwitchFlipKeepsOtherFields(t *testing.T) {
	old := &shelly.Status{Switch0: &sswitch.Status{Id: 0, Output: false, Apower: 12.5}}
	d := newStatusDevice(t, old)

	changed, err := d.ApplyStatusNotification(json.RawMessage(`{"ts":1,"switch:0":{"id":0,"output":true,"source":"HTTP_in"}}`))
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v, want true,nil", changed, err)
	}
	got := d.Status().Switch0
	if !got.Output || got.Source != "HTTP_in" || got.Apower != 12.5 {
		t.Errorf("merged switch = %+v", got)
	}
	if old.Switch0.Output {
		t.Error("published snapshot was mutated in place")
	}
}

func TestApplyStatusNotification_MeasurementOnlyIsNotAChange(t *testing.T) {
	d := newStatusDevice(t, &shelly.Status{Switch0: &sswitch.Status{Output: true}})
	changed, err := d.ApplyStatusNotification(json.RawMessage(`{"switch:0":{"id":0,"apower":40.1}}`))
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v, want false,nil", changed, err)
	}
	if s := d.Status().Switch0; !s.Output || s.Apower != 40.1 {
		t.Errorf("switch = %+v", s)
	}
}

func TestApplyStatusNotification_InputAndIgnoredComponents(t *testing.T) {
	d := newStatusDevice(t, &shelly.Status{Input0: nil})
	changed, err := d.ApplyStatusNotification(json.RawMessage(`{"input:0":{"id":0,"state":true},"cloud":{"connected":true}}`))
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if !d.Status().Input0.State {
		t.Error("input state not applied")
	}
	if changed, _ = d.ApplyStatusNotification(json.RawMessage(`{"input:0":{"id":0,"state":true}}`)); changed {
		t.Error("same state reported as a change")
	}
}

func TestApplyStatusNotification_NoStatusYet(t *testing.T) {
	d := &Device{log: logr.Discard()}
	changed, err := d.ApplyStatusNotification(json.RawMessage(`{"switch:0":{"output":true}}`))
	if err != nil || changed || d.Status() != nil {
		t.Fatalf("changed=%v err=%v status=%v, want untouched", changed, err, d.Status())
	}
}

func TestApplyStatusNotification_Malformed(t *testing.T) {
	d := newStatusDevice(t, &shelly.Status{})
	if _, err := d.ApplyStatusNotification(json.RawMessage(`{"switch:0":{"output":"yes"}}`)); err == nil {
		t.Error("expected error for a malformed component")
	}
}

// TestApplyStatusNotification_Race runs merges, refresh-style replacement and
// readers concurrently; meaningful under -race. The reader iterates the slice
// elements, so a merge that wrote into a slice shared with a published
// snapshot (json.Unmarshal reuses the backing array) is reported as a race.
func TestApplyStatusNotification_Race(t *testing.T) {
	d := newStatusDevice(t, &shelly.Status{Switch0: &sswitch.Status{
		Errors: []string{"a"},
		Aenergy: struct {
			Total    float32   `json:"total"`
			ByMinute []float32 `json:"by_minute"`
			MinuteTs int       `json:"minute_ts"`
		}{ByMinute: []float32{1, 2, 3}},
	}})
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_, _ = d.ApplyStatusNotification(json.RawMessage(`{"switch:0":{"output":true,"errors":["b"],"aenergy":{"by_minute":[4,5,6]}}}`))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_, _ = d.ApplyStatusNotification(json.RawMessage(`{"switch:0":{"output":false}}`))
		}
	}()
	go func() {
		defer wg.Done()
		sum := float32(0)
		n := 0
		for i := 0; i < 500; i++ {
			if s := d.Status(); s != nil && s.Switch0 != nil {
				for _, v := range s.Switch0.Aenergy.ByMinute {
					sum += v
				}
				for _, e := range s.Switch0.Errors {
					n += len(e)
				}
			}
		}
		_, _ = sum, n
	}()
	wg.Wait()
}

func TestApplyStatusNotification_AllComponentIndexes(t *testing.T) {
	d := newStatusDevice(t, &shelly.Status{})
	changed, err := d.ApplyStatusNotification(json.RawMessage(`{"switch:1":{"output":true},"switch:2":{"output":true},"switch:3":{"output":true},"input:1":{"state":true},"input:2":{"state":true},"input:3":{"state":true}}`))
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	s := d.Status()
	if !s.Switch1.Output || !s.Switch2.Output || !s.Switch3.Output || !s.Input1.State || !s.Input2.State || !s.Input3.State {
		t.Errorf("not all indexes applied: %+v", s)
	}
}
