package occupancy

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/myhome/mqtt"
	"github.com/asnowfix/home-automation/pkg/sfr"

	"github.com/go-logr/logr"
)

// fakeLanChecker is an in-test implementation of LanChecker.
type fakeLanChecker struct {
	hosts []*sfr.LanHost
	err   error
}

func (f *fakeLanChecker) GetHostsList(_ context.Context) ([]*sfr.LanHost, error) {
	return f.hosts, f.err
}

// newTestService creates an occupancy Service wired with a cancellable context,
// a recording MQTT mock, and the given LanChecker.
// The returned cancel function should be called to stop the service context.
func newTestService(t *testing.T, checker LanChecker) (*Service, *mqtt.RecordingMockClient, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	mc := mqtt.NewRecordingMockClient()
	svc := &Service{
		ctx:              ctx,
		log:              logr.Discard(),
		mc:               mc,
		lanChecker:       checker,
		lastSeenWindow:   5 * time.Minute,
		mobilePollPeriod: time.Hour, // large enough that polling won't fire during tests
		mobileDevices:    []string{"iphone"},
	}
	return svc, mc, cancel
}

// --- IsOccupied ---

func TestIsOccupied_InitiallyFalse(t *testing.T) {
	svc, _, cancel := newTestService(t, &fakeLanChecker{})
	defer cancel()

	if svc.IsOccupied(context.Background()) {
		t.Error("newly created service should not be occupied")
	}
}

func TestIsOccupied_RecentInputEvent(t *testing.T) {
	svc, _, cancel := newTestService(t, &fakeLanChecker{})
	defer cancel()

	svc.lastEvent.Store(time.Now().UnixNano())
	if !svc.IsOccupied(context.Background()) {
		t.Error("recent input event should make service occupied")
	}
}

func TestIsOccupied_StaleInputEvent(t *testing.T) {
	svc, _, cancel := newTestService(t, &fakeLanChecker{})
	defer cancel()

	stale := time.Now().Add(-(svc.lastSeenWindow + time.Second)).UnixNano()
	svc.lastEvent.Store(stale)

	if svc.IsOccupied(context.Background()) {
		t.Error("stale input event should not make service occupied")
	}
}

func TestIsOccupied_RecentMobileDevice(t *testing.T) {
	svc, _, cancel := newTestService(t, &fakeLanChecker{})
	defer cancel()

	svc.lastMobileSeen.Store(time.Now().UnixNano())
	if !svc.IsOccupied(context.Background()) {
		t.Error("recently seen mobile device should make service occupied")
	}
}

func TestIsOccupied_StaleMobileDevice(t *testing.T) {
	svc, _, cancel := newTestService(t, &fakeLanChecker{})
	defer cancel()

	stale := time.Now().Add(-(svc.lastSeenWindow + time.Second)).UnixNano()
	svc.lastMobileSeen.Store(stale)

	if svc.IsOccupied(context.Background()) {
		t.Error("stale mobile device should not make service occupied")
	}
}

func TestIsOccupied_BothSourcesRecent(t *testing.T) {
	svc, _, cancel := newTestService(t, &fakeLanChecker{})
	defer cancel()

	now := time.Now().UnixNano()
	svc.lastEvent.Store(now)
	svc.lastMobileSeen.Store(now)

	if !svc.IsOccupied(context.Background()) {
		t.Error("both recent sources should make service occupied")
	}
}

// --- checkMobilePresence ---

func TestCheckMobilePresence_MatchingDeviceOnline(t *testing.T) {
	checker := &fakeLanChecker{
		hosts: []*sfr.LanHost{
			{Name: "MyIphone", Ip: net.ParseIP("192.168.1.50"), Status: "online", Alive: 1},
		},
	}
	svc, mc, cancel := newTestService(t, checker)
	defer cancel()

	before := time.Now().UnixNano()
	svc.checkMobilePresence()
	after := time.Now().UnixNano()

	stored := svc.lastMobileSeen.Load()
	if stored < before || stored > after {
		t.Errorf("lastMobileSeen should be in [before, after], got %d", stored)
	}
	// Should have published an occupancy message.
	if len(mc.Published("myhome/occupancy")) == 0 {
		t.Error("expected occupancy message published")
	}
}

func TestCheckMobilePresence_MatchingDeviceOnline_PayloadOccupied(t *testing.T) {
	checker := &fakeLanChecker{
		hosts: []*sfr.LanHost{
			{Name: "MyIphone", Status: "online", Alive: 1},
		},
	}
	svc, mc, cancel := newTestService(t, checker)
	defer cancel()

	svc.checkMobilePresence()

	payloads := mc.Published("myhome/occupancy")
	if len(payloads) == 0 {
		t.Fatal("expected at least one publish to myhome/occupancy")
	}
	var v struct {
		Occupied bool   `json:"occupied"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(payloads[0], &v); err != nil {
		t.Fatalf("unmarshal occupancy: %v", err)
	}
	if !v.Occupied {
		t.Error("expected occupied=true when mobile device is online")
	}
}

func TestCheckMobilePresence_NoMatchingDevice(t *testing.T) {
	checker := &fakeLanChecker{
		hosts: []*sfr.LanHost{
			{Name: "desktop-pc", Status: "online", Alive: 1},
		},
	}
	svc, _, cancel := newTestService(t, checker)
	defer cancel()

	svc.checkMobilePresence()

	if svc.lastMobileSeen.Load() != 0 {
		t.Error("non-matching device should not update lastMobileSeen")
	}
}

func TestCheckMobilePresence_MatchingDeviceOffline(t *testing.T) {
	checker := &fakeLanChecker{
		hosts: []*sfr.LanHost{
			// Name matches the pattern but device is offline.
			{Name: "MyIphone", Status: "offline", Alive: 0},
		},
	}
	svc, _, cancel := newTestService(t, checker)
	defer cancel()

	svc.checkMobilePresence()

	if svc.lastMobileSeen.Load() != 0 {
		t.Error("offline matching device should not update lastMobileSeen")
	}
}

func TestCheckMobilePresence_CaseInsensitiveMatch(t *testing.T) {
	// Pattern is "iphone" (lowercase); host name is "IPHONE" (upper).
	checker := &fakeLanChecker{
		hosts: []*sfr.LanHost{
			{Name: "IPHONE", Status: "online", Alive: 1},
		},
	}
	svc, _, cancel := newTestService(t, checker)
	defer cancel()

	svc.checkMobilePresence()

	if svc.lastMobileSeen.Load() == 0 {
		t.Error("case-insensitive match should update lastMobileSeen")
	}
}

func TestCheckMobilePresence_LanCheckerError(t *testing.T) {
	checker := &fakeLanChecker{err: context.DeadlineExceeded}
	svc, _, cancel := newTestService(t, checker)
	defer cancel()

	// Should not panic; error is logged and ignored.
	svc.checkMobilePresence()

	if svc.lastMobileSeen.Load() != 0 {
		t.Error("LAN checker error should not update lastMobileSeen")
	}
}
