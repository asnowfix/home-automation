package occupancy

import (
	"context"
	"encoding/json"
	"fmt"
	"myhome/mqtt"
	mqttclient "myhome/mqtt"
	"pkg/sfr"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
)

// Service implements a simple HTTP server that reports home occupancy based on
// recent motion/input events observed on MQTT and mobile device presence on the LAN.
//
// Heuristic:
// - Subscribe to Shelly Gen2 events: "+/events/rpc" for NotifyStatus messages
//   containing input state changes (e.g., button presses, motion sensors)
// - Any NotifyStatus event with "input:N" updates the lastEvent timestamp
// - Poll SFR Box LAN hosts for devices whose names match configured mobile devices
// - Occupied if:
//   * now - lastEvent <= window, OR
//   * Mobile device seen online in the past window
//
// HTTP:
// - GET /status -> {"occupied":true|false,"reason":"input|mobile|mobile+input|none"}
// - Optional query: window=<seconds> to override window for this check only.

type Service struct {
	ctx              context.Context
	log              logr.Logger
	mc               mqttclient.Client
	lastEvent        atomic.Int64 // unix nano of last relevant input event
	lastMobileSeen   atomic.Int64 // unix nano of last mobile device presence
	lastSeenTime     atomic.Int64 // unix nano of last seen event (for timer expiry message)
	lastSeenTimer    *time.Timer  // timer for publishing false occupancy
	lastSeenWindow   time.Duration
	mobilePollPeriod time.Duration
	mobileDevices    []string // list of device name patterns to check for (case-insensitive substring match)
}

func NewService(ctx context.Context, log logr.Logger, mc mqttclient.Client, window time.Duration, mobilePollPeriod time.Duration, mobileDevices []string) *Service {
	s := &Service{
		ctx:              ctx,
		log:              log.WithName("occupancy.Service"),
		mc:               mc,
		lastSeenWindow:   window,
		mobilePollPeriod: mobilePollPeriod,
		mobileDevices:    mobileDevices,
	}
	return s
}

// SetWindow configures the time window for input devices & mobile device presence detection.
// If a mobile device was seen online within this window, the home is considered occupied.
func (s *Service) SetWindow(window time.Duration) *Service {
	s.lastSeenWindow = window
	return s
}

// SetMobilePollPeriod configures how often to poll the SFR Box for mobile device presence.
func (s *Service) SetMobilePollPeriod(period time.Duration) *Service {
	s.mobilePollPeriod = period
	return s
}

// SetMobileDevices configures the list of device name patterns to check for presence.
// Device names are matched case-insensitively using substring matching.
// Example: []string{"iPhone", "iPad", "Android"}
func (s *Service) SetMobileDevices(devices []string) *Service {
	s.mobileDevices = devices
	return s
}

// Start runs the MQTT subscriptions, mobile device polling
func (s *Service) Start() error {
	// Subscribe to Shelly Gen2 input status topics
	go s.subscribeInputs()

	// Start mobile device presence polling
	go s.pollMobilePresence()

	go func() {
		<-s.ctx.Done()
		if s.lastSeenTimer != nil {
			s.lastSeenTimer.Stop()
		}
	}()

	s.log.Info("Occupancy service started", "lastSeenWindow", s.lastSeenWindow, "mobilePollPeriod", s.mobilePollPeriod, "mobileDevices", s.mobileDevices)
	return nil
}

type occupancy struct {
	Occupied bool   `json:"occupied"`
	Reason   string `json:"reason,omitempty"`
}

func (s *Service) occupied(reason string) {
	// Stop existing timer if any
	if s.lastSeenTimer != nil {
		s.lastSeenTimer.Stop()
		s.lastSeenTimer = nil
	}

	status := occupancy{
		Occupied: true,
		Reason:   reason,
	}
	b, err := json.Marshal(status)
	if err != nil {
		s.log.Error(err, "Failed to marshal occupancy")
		return
	}
	s.mc.Publish(s.ctx, "myhome/occupancy", b, mqtt.AtLeastOnce, true /*retain*/, "myhome/occupancy")

	s.lastSeenTime.Store(time.Now().UnixNano())
	s.lastSeenTimer = time.AfterFunc(s.lastSeenWindow, func() {
		// Timer expired - publish false occupancy with elapsed time
		lastSeen := time.Unix(0, s.lastSeenTime.Load())
		elapsed := time.Since(lastSeen)
		hours := int(elapsed.Hours())
		minutes := int(elapsed.Minutes()) % 60
		seconds := int(elapsed.Seconds()) % 60

		var elapsedStr string
		if hours > 0 {
			elapsedStr = fmt.Sprintf("%dh %dm %ds ago", hours, minutes, seconds)
		} else if minutes > 0 {
			elapsedStr = fmt.Sprintf("%dm %ds ago", minutes, seconds)
		} else {
			elapsedStr = fmt.Sprintf("%ds ago", seconds)
		}

		status := occupancy{
			Occupied: false,
			Reason:   fmt.Sprintf("last seen %s", elapsedStr),
		}
		b, err := json.Marshal(status)
		if err != nil {
			s.log.Error(err, "Failed to marshal occupancy on timer expiry")
			return
		}
		s.mc.Publish(s.ctx, "myhome/occupancy", b, mqtt.AtLeastOnce, true /*retain*/, "myhome/occupancy")
		s.log.Info("Published false occupancy after timer expiry", "elapsed", elapsedStr)
	})
}

func (s *Service) subscribeInputs() {
	// Gen2 events/rpc for NotifyStatus with input activity
	topic := "+/events/rpc"
	ch, err := s.mc.SubscribeWithTopic(s.ctx, topic, 8, "myhome/occupancy")
	if err != nil {
		s.log.Error(err, "Failed to subscribe to events", "topic", topic)
		return
	}
	s.log.Info("Subscribed to events", "topic", topic)

	// Monitor for NotifyStatus events containing input state changes
	// Example: {"method":"NotifyStatus","params":{"input:0":{"id":0,"state":true}}}
	for {
		select {
		case <-s.ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			// Filter for NotifyStatus events with input state changes
			payload := string(msg.Payload())
			// s.log.Info("Received event", "payload", payload)
			if strings.Contains(payload, "\"NotifyStatus\"") && strings.Contains(payload, "\"input:") {
				s.log.Info("Received event with input state change", "payload", payload)
				s.lastEvent.Store(time.Now().UnixNano())
				s.occupied(fmt.Sprintf("input change: %s", payload))
			}
		}
	}
}

func (s *Service) pollMobilePresence() {
	ticker := time.NewTicker(s.mobilePollPeriod)
	defer ticker.Stop()

	// Do an immediate check on startup
	s.checkMobilePresence()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.checkMobilePresence()
		}
	}
}

func (s *Service) checkMobilePresence() {
	hosts, err := sfr.GetHostsList()
	if err != nil {
		s.log.Error(err, "Failed to get SFR LAN hosts")
		return
	}

	// Look for any host whose name matches one of the configured mobile devices
	for _, host := range *hosts {
		hostNameLower := strings.ToLower(host.Name)
		for _, devicePattern := range s.mobileDevices {
			if strings.Contains(hostNameLower, strings.ToLower(devicePattern)) {
				// Check if device is online (status="online" or alive > 0)
				if strings.ToLower(host.Status) == "online" || host.Alive > 0 {
					s.log.Info("Mobile device detected online", "name", host.Name, "pattern", devicePattern, "ip", host.Ip, "mac", host.Mac, "status", host.Status, "alive", host.Alive)
					s.lastMobileSeen.Store(time.Now().UnixNano())
					s.occupied(fmt.Sprintf("seen mobile: %s (%s) on %s", host.Name, host.Mac, time.Now().Format("2006-01-02 15:04:05")))
					return
				}
			}
		}
	}

	s.log.V(1).Info("No mobile devices found online", "patterns", s.mobileDevices)
}

// Start launches the occupancy HTTP service listening on port with the given MQTT client.
func Start(ctx context.Context, port int, mc mqttclient.Client, window time.Duration, mobilePollPeriod time.Duration, mobileDevices []string) error {
	log := logr.FromContextOrDiscard(ctx)
	svc := NewService(ctx, log, mc, window, mobilePollPeriod, mobileDevices)
	if mobileDevices != nil {
		svc.SetMobileDevices(mobileDevices)
	}
	return svc.Start()
}
