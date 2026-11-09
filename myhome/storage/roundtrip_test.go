package storage

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/asnowfix/home-automation/pkg/shelly/shelly"
)

// TestExportImportRoundTrip_Devices: devices read back from one database, as
// the export serialises them, then written into an empty one and read back
// again, must serialise identically. Covers room_id, info, a config revision
// and a MAC-less (BLU-like) record. (#775 step 3, devices section only.)
func TestExportImportRoundTrip_Devices(t *testing.T) {
	ctx := context.Background()
	src := newTestStorage(t)

	withInfo := makeDevice("Shelly", "shellyplus1-aaa", "aa:bb:cc:dd:ee:01", "hall-light", "hall-light.local")
	withInfo.RoomId = "hall"
	withInfo.ConfigRevision = 7
	withInfo.Info = &shelly.DeviceInfo{Id: "shellyplus1-aaa"}
	blu := makeDevice("Shelly", "shellyblu-112233445566", "", "salon-temperature", "")
	blu.RoomId = "living-room"
	noRoom := makeDevice("Shelly", "shellyplus1-bbb", "aa:bb:cc:dd:ee:02", "garage", "garage.local")
	for _, d := range []*myhome.Device{withInfo, blu, noRoom} {
		if _, err := src.SetDevice(ctx, d, true); err != nil {
			t.Fatalf("SetDevice: %v", err)
		}
	}

	export := func(s *DeviceStorage) []byte {
		ds, err := s.GetAllDevices(ctx)
		if err != nil {
			t.Fatalf("GetAllDevices: %v", err)
		}
		sort.Slice(ds, func(i, j int) bool { return ds[i].Id() < ds[j].Id() })
		b, err := json.Marshal(ds)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		return b
	}
	first := export(src)

	var imported []myhome.Device
	if err := json.Unmarshal(first, &imported); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(imported) != 3 {
		t.Fatalf("expected 3 devices in the export, got %d: %s", len(imported), first)
	}
	dst := newTestStorage(t)
	for i := range imported {
		if _, err := dst.SetDevice(ctx, &imported[i], true); err != nil {
			t.Fatalf("import SetDevice %s: %v", imported[i].Id(), err)
		}
	}

	if second := export(dst); string(first) != string(second) {
		t.Errorf("round trip changed the export:\nfirst:  %s\nsecond: %s", first, second)
	}
	if !strings.Contains(string(first), `"room_id":"living-room"`) {
		t.Errorf("export lacks the BLU room assignment: %s", first)
	}
}
