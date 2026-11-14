package myhome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func dev(id, mac, room string) *Device {
	d := &Device{}
	d.Manufacturer_ = "Shelly"
	d.Id_ = id
	d.MAC = mac
	d.RoomId = room
	return d
}

func calls(f *FakeClient, v Verb) []FakeCall {
	var out []FakeCall
	for _, c := range f.Calls {
		if c.Method == v {
			out = append(out, c)
		}
	}
	return out
}

func exportOf(ds ...*Device) *[]*Device { return &ds }

// device.setroom's result crosses the wire as JSON and is decoded the way the
// client does it (CallE): into the value NewResult returns.
func TestDeviceSetRoomResult_WireDecoding(t *testing.T) {
	decode := func(payload string) any {
		result := signatures[DeviceSetRoom].NewResult()
		if err := json.Unmarshal([]byte(payload), &result); err != nil {
			t.Fatalf("%s: %v", payload, err)
		}
		return result
	}
	// An older daemon answers null: no result, which the import counts as "unknown".
	if got := decode("null"); got != nil {
		if r, ok := got.(*DeviceSetRoomResult); !ok || r.KVS != "" {
			t.Errorf("null decoded to %#v", got)
		}
	}
	r, ok := decode(`{"kvs":"unreachable","kvs_error":"timeout"}`).(*DeviceSetRoomResult)
	if !ok || r.KVS != KVSUnreachable || r.KVSError != "timeout" {
		t.Errorf("struct decoded to %#v", r)
	}
	// And the other way round: the daemon's result survives marshalling.
	b, _ := json.Marshal(&DeviceSetRoomResult{KVS: KVSNotApplicable})
	if r, ok := decode(string(b)).(*DeviceSetRoomResult); !ok || r.KVS != KVSNotApplicable {
		t.Errorf("round trip lost the outcome: %s", b)
	}
	// A null answer is reported as unknown, not as a failure.
	f := NewFakeClient()
	f.SetResult(DeviceExport, exportOf(dev("a", "", "old")))
	f.SetResult(DeviceSetRoom, nil)
	rep, err := ImportDatabase(context.Background(), f, &DatabaseDump{Devices: []*Device{dev("a", "", "new")}}, ImportOptions{})
	if err != nil || rep.RoomKVS.Unknown != 1 || rep.Devices.Updated != 1 {
		t.Errorf("null result: %+v %v", rep, err)
	}
}

func TestImport_MACMatching(t *testing.T) {
	ctx := context.Background()
	t.Run("normalised and unique", func(t *testing.T) {
		f := NewFakeClient()
		f.SetResult(DeviceExport, exportOf(dev("new-id", "AA:BB:CC:DD:EE:01", "x")))
		f.SetResult(DeviceSetRoom, &DeviceSetRoomResult{KVS: KVSSet})
		rep, err := ImportDatabase(ctx, f, &DatabaseDump{Devices: []*Device{dev("old-id", "aa-bb-cc-dd-ee-01", "y")}}, ImportOptions{})
		if err != nil || rep.Devices.Updated != 1 {
			t.Fatalf("%+v %v", rep, err)
		}
		if p := calls(f, DeviceSetRoom)[0].Params.(*DeviceSetRoomParams); p.Identifier != "new-id" || p.RoomId != "y" {
			t.Errorf("wrong target: %+v", p)
		}
	})
	t.Run("duplicate MAC in target is skipped", func(t *testing.T) {
		f := NewFakeClient()
		f.SetResult(DeviceExport, exportOf(dev("a", "aa:bb:cc:dd:ee:01", "x"), dev("b", "AA:BB:CC:DD:EE:01", "x")))
		rep, err := ImportDatabase(ctx, f, &DatabaseDump{Devices: []*Device{dev("c", "aa:bb:cc:dd:ee:01", "y")}}, ImportOptions{CreateMissing: true})
		if err != nil || rep.Devices.Skipped != 1 || rep.Devices.Created != 0 {
			t.Fatalf("%+v %v", rep, err)
		}
		if len(calls(f, DeviceSetRoom))+len(calls(f, DeviceUpdate)) != 0 {
			t.Error("an ambiguous MAC must not write anything")
		}
	})
	t.Run("duplicate MAC in dump is skipped", func(t *testing.T) {
		f := NewFakeClient()
		f.SetResult(DeviceExport, exportOf(dev("a", "aa:bb:cc:dd:ee:01", "x")))
		rep, _ := ImportDatabase(ctx, f, &DatabaseDump{Devices: []*Device{dev("c", "aa:bb:cc:dd:ee:01", "y"), dev("d", "aa:bb:cc:dd:ee:01", "z")}}, ImportOptions{})
		if rep.Devices.Skipped != 2 || len(calls(f, DeviceSetRoom)) != 0 {
			t.Errorf("%+v", rep)
		}
	})
}

func TestImport_CreatedDeviceWhoseRoomFailsIsNotCounted(t *testing.T) {
	f := NewFakeClient()
	f.SetResult(DeviceExport, exportOf())
	f.SetResult(DeviceUpdate, nil)
	f.SetError(DeviceSetRoom, errors.New("boom (code:1)"))
	rep, err := ImportDatabase(context.Background(), f, &DatabaseDump{Devices: []*Device{dev("a", "", "hall")}}, ImportOptions{CreateMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Devices.Created != 0 || rep.Devices.Failed != 1 || !rep.Incomplete() {
		t.Errorf("%+v", rep.Devices)
	}
}

func TestImport_RoomEmptyFieldsLeaveTargetAlone(t *testing.T) {
	ctx := context.Background()
	have := &TemperatureRoomConfig{RoomID: "hall", Name: "Hall", Kinds: []RoomKind{RoomKindOffice},
		Levels: map[string]float64{"eco": 17}, ICalURL: "https://calendar.example.org/hall.ics"}
	f := NewFakeClient()
	f.SetResult(TemperatureList, &TemperatureRoomList{"hall": have})

	// Emptied ical_url, name and kinds in the dump: nothing to do, nothing written.
	rep, err := ImportDatabase(ctx, f, &DatabaseDump{TemperatureRooms: map[string]*TemperatureRoomConfig{
		"hall": {RoomID: "hall", Levels: map[string]float64{"eco": 17}}}}, ImportOptions{})
	if err != nil || rep.Rooms.Unchanged != 1 || len(calls(f, RoomEdit)) != 0 {
		t.Fatalf("%+v %v", rep.Rooms, err)
	}

	// Different levels with an empty name: edit sent without a name, ical untouched.
	f.SetResult(RoomEdit, &RoomEditResult{Success: true})
	rep, err = ImportDatabase(ctx, f, &DatabaseDump{TemperatureRooms: map[string]*TemperatureRoomConfig{
		"hall": {RoomID: "hall", Levels: map[string]float64{"eco": 16}}}}, ImportOptions{})
	if err != nil || rep.Rooms.Updated != 1 {
		t.Fatalf("%+v %v", rep.Rooms, err)
	}
	e := calls(f, RoomEdit)[0].Params.(*RoomEditParams)
	if e.Name != nil || e.ICalURL != nil || e.Levels["eco"] != 16 {
		t.Errorf("unexpected edit %+v", e)
	}
}

// A silent daemon or a cancelled context stops the import, but the partial
// report is returned so that the CLI can print what was done.
func TestImport_FatalErrorsReturnPartialReport(t *testing.T) {
	dump := &DatabaseDump{Devices: []*Device{dev("a", "", "new"), dev("b", "", "new")}}
	exp := exportOf(dev("a", "", "old"), dev("b", "", "old"))

	t.Run("timeout", func(t *testing.T) {
		f := NewFakeClient()
		f.SetResult(DeviceExport, exp)
		f.SetError(DeviceSetRoom, fmt.Errorf("%w to method device.setroom", ErrRPCTimeout))
		rep, err := ImportDatabase(context.Background(), f, dump, ImportOptions{})
		if !errors.Is(err, ErrRPCTimeout) || rep == nil {
			t.Fatalf("got %v, %v", rep, err)
		}
		if n := len(calls(f, DeviceSetRoom)); n != 1 {
			t.Errorf("must stop at the first timeout, made %d calls", n)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		f := NewFakeClient()
		f.SetResult(DeviceExport, exp)
		f.SetError(DeviceSetRoom, context.Canceled)
		cancel()
		rep, err := ImportDatabase(ctx, f, dump, ImportOptions{})
		if !errors.Is(err, context.Canceled) || rep == nil {
			t.Fatalf("got %v, %v", rep, err)
		}
	})
	t.Run("rooms done before devices time out", func(t *testing.T) {
		f := NewFakeClient()
		f.SetResult(TemperatureList, &TemperatureRoomList{})
		f.SetResult(RoomCreate, &RoomCreateResult{Success: true})
		f.SetResult(RoomEdit, &RoomEditResult{Success: true})
		f.SetResult(DeviceExport, exp)
		f.SetError(DeviceSetRoom, fmt.Errorf("%w", ErrRPCTimeout))
		d := *dump
		d.TemperatureRooms = map[string]*TemperatureRoomConfig{"hall": {Name: "Hall", Kinds: []RoomKind{RoomKindOther}, Levels: map[string]float64{"eco": 17}}}
		rep, err := ImportDatabase(context.Background(), f, &d, ImportOptions{})
		if err == nil || rep.Rooms.Created != 1 {
			t.Errorf("partial report lost: %+v %v", rep.Rooms, err)
		}
	})
}
