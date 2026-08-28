package health

import (
	"sync"
	"time"
)

type ErrorCategory string

const (
	ErrorNone          ErrorCategory = ""
	ErrorConfiguration ErrorCategory = "configuration"
	ErrorAuthorization ErrorCategory = "authorization"
	ErrorNetwork       ErrorCategory = "network"
	ErrorBroker        ErrorCategory = "mqtt"
	ErrorMedia         ErrorCategory = "media"
)

type Snapshot struct {
	Status           string        `json:"status"`
	Ready            bool          `json:"ready"`
	UptimeSeconds    int64         `json:"uptime_seconds"`
	CredentialsReady bool          `json:"credentials_ready"`
	BridgesReady     int           `json:"bridges_ready"`
	BridgesTotal     int           `json:"bridges_total"`
	Go2RTCRequired   bool          `json:"go2rtc_required"`
	Go2RTCReady      bool          `json:"go2rtc_ready"`
	MQTTEnabled      bool          `json:"mqtt_enabled"`
	MQTTConnected    bool          `json:"mqtt_connected"`
	ReconnectsTotal  uint64        `json:"reconnects_total"`
	ActiveSessions   int64         `json:"active_sessions"`
	LastError        ErrorCategory `json:"last_error,omitempty"`
}

type State struct {
	mu       sync.RWMutex
	started  time.Time
	live     bool
	snapshot Snapshot
}

func NewState(started time.Time) *State { return &State{started: started} }

func (s *State) SetLive(value bool) {
	s.mu.Lock()
	s.live = value
	s.mu.Unlock()
}

func (s *State) SetCredentialsReady(value bool) {
	s.mu.Lock()
	s.snapshot.CredentialsReady = value
	s.mu.Unlock()
}

func (s *State) SetBridges(ready, total int) {
	s.mu.Lock()
	s.snapshot.BridgesReady = ready
	s.snapshot.BridgesTotal = total
	s.mu.Unlock()
}

func (s *State) SetGo2RTC(required, ready bool) {
	s.mu.Lock()
	s.snapshot.Go2RTCRequired = required
	s.snapshot.Go2RTCReady = ready
	s.mu.Unlock()
}

func (s *State) SetMQTT(enabled, connected bool) {
	s.mu.Lock()
	s.snapshot.MQTTEnabled = enabled
	s.snapshot.MQTTConnected = connected
	s.mu.Unlock()
}

func (s *State) SetCounters(reconnects uint64, activeSessions int64) {
	s.mu.Lock()
	s.snapshot.ReconnectsTotal = reconnects
	s.snapshot.ActiveSessions = activeSessions
	s.mu.Unlock()
}

func (s *State) SetLastError(category ErrorCategory) {
	s.mu.Lock()
	s.snapshot.LastError = category
	s.mu.Unlock()
}

func (s *State) Live() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.live
}

// Healthy backs the liveness endpoint the Home Assistant watchdog polls. A
// process that is up but serving no camera at all is not healthy — that is the
// state the watchdog exists to catch. A partial outage stays healthy on
// purpose: the runtime restarts individual bridges itself, and restarting the
// whole add-on would drop the cameras that are still streaming.
func (s *State) Healthy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.live {
		return false
	}
	return s.snapshot.BridgesTotal == 0 || s.snapshot.BridgesReady > 0
}

func (s *State) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := s.snapshot
	snapshot.Ready = s.live && snapshot.CredentialsReady &&
		snapshot.BridgesTotal > 0 && snapshot.BridgesReady == snapshot.BridgesTotal &&
		(!snapshot.Go2RTCRequired || snapshot.Go2RTCReady)
	if s.live {
		snapshot.Status = "ok"
	} else {
		snapshot.Status = "starting"
	}
	snapshot.UptimeSeconds = int64(time.Since(s.started).Seconds())
	if snapshot.UptimeSeconds < 0 {
		snapshot.UptimeSeconds = 0
	}
	return snapshot
}
