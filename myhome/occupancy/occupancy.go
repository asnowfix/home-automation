package occupancy

import (
	"context"
	"encoding/json"
	"fmt"
	mqttclient "myhome/mqtt"
	"net/http"
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
	mc               *mqttclient.Client
	httpSrv          *http.Server
	lastEvent        atomic.Int64 // unix nano of last relevant input event
	lastMobileSeen   atomic.Int64 // unix nano of last mobile device presence
	window           time.Duration
	mobilePollPeriod time.Duration
	mobileDevices    []string // list of device name patterns to check for (case-insensitive substring match)
}

func NewService(ctx context.Context, log logr.Logger, mc *mqttclient.Client, window time.Duration, mobilePollPeriod time.Duration, mobileDevices []string) *Service {
	s := &Service{
		ctx:              ctx,
		log:              log.WithName("occupancy.Service"),
		mc:               mc,
		window:           window,
		mobilePollPeriod: mobilePollPeriod,
		mobileDevices:    mobileDevices,
	}
	return s
}

// SetWindow configures the time window for input devices & mobile device presence detection.
// If a mobile device was seen online within this window, the home is considered occupied.
func (s *Service) SetWindow(window time.Duration) *Service {
	s.window = window
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

// Start runs the MQTT subscriptions, mobile device polling, and the HTTP server on the given port.
func (s *Service) Start(port int) error {
	// Subscribe to Shelly Gen2 input status topics
	go s.subscribeInputs()

	// Start mobile device presence polling
	go s.pollMobilePresence()

	mux := http.NewServeMux()
	mux.HandleFunc("/status", s.handleStatus)

	s.httpSrv = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		<-s.ctx.Done()
		_ = s.httpSrv.Close()
	}()

	go func() {
		if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Error(err, "occupancy HTTP server crashed")
		}
	}()

	s.log.Info("Occupancy HTTP service started", "port", port, "window", s.window, "mobilePollPeriod", s.mobilePollPeriod, "mobileDevices", s.mobileDevices)
	return nil
}

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	// Optional per-request window override
	win := s.window
	if v := r.URL.Query().Get("window"); v != "" {
		if secs, err := time.ParseDuration(v + "s"); err == nil {
			win = secs
		}
	}
	occupied, reason := s.isOccupied(win)
	resp := map[string]any{
		"occupied": occupied,
		"reason":   reason,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Service) isOccupied(window time.Duration) (bool, string) {
	// Check both conditions
	last := time.Unix(0, s.lastEvent.Load())
	hasInput := !last.IsZero() && time.Since(last) <= window

	lastMobile := time.Unix(0, s.lastMobileSeen.Load())
	hasMobile := !lastMobile.IsZero() && time.Since(lastMobile) <= s.window

	// Return appropriate reason
	if hasInput && hasMobile {
		return true, "mobile+input"
	} else if hasInput {
		return true, "input"
	} else if hasMobile {
		return true, "mobile"
	}

	return false, "none"
}

func (s *Service) subscribeInputs() {
	// Gen2 events/rpc for NotifyStatus with input activity
	topic := "+/events/rpc"
	ch, err := s.mc.MultiSubscribe(s.ctx, topic, 8, "myhome/gen2")
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
					return
				}
			}
		}
	}

	s.log.V(1).Info("No mobile devices found online", "patterns", s.mobileDevices)
}

// Start launches the occupancy HTTP service listening on port with the given MQTT client.
func Start(ctx context.Context, port int, mc *mqttclient.Client, window time.Duration, mobilePollPeriod time.Duration, mobileDevices []string) error {
	log := logr.FromContextOrDiscard(ctx)
	svc := NewService(ctx, log, mc, window, mobilePollPeriod, mobileDevices)
	if mobileDevices != nil {
		svc.SetMobileDevices(mobileDevices)
	}
	return svc.Start(port)
}
