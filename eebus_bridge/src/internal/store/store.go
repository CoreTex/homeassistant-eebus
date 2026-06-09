// Package store holds the live, thread-safe snapshot of all EEBUS data that the
// Home Assistant integration consumes. It is the single source of truth shared
// between the EEBUS layer (writers) and the HTTP/WebSocket API (readers).
package store

import (
	"sync"
	"time"
)

// ServiceInfo describes this local EEBUS service.
type ServiceInfo struct {
	SKI         string `json:"ski"`
	ShipID      string `json:"ship_id"`
	Version     string `json:"version"`
	DeviceName  string `json:"device_name"`
	Fingerprint string `json:"fingerprint"`
	QRCode      string `json:"qr_code"`
}

// Connection is a remote EEBUS service we know about.
type Connection struct {
	SKI       string `json:"ski"`
	Name      string `json:"name"`
	Brand     string `json:"brand"`
	Model     string `json:"model"`
	Connected bool   `json:"connected"`
	Trusted   bool   `json:"trusted"`
}

// LimitState is a single LPC/LPP load limit as held by the Controllable System.
// While Active is true, automations should keep consumers curtailed.
type LimitState struct {
	Active     bool       `json:"active"`
	ValueW     float64    `json:"value_w"`
	DurationS  float64    `json:"duration_s"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	Changeable bool       `json:"changeable"`
}

// LPCState is the Controllable-System view of "Limitation of Power Consumption".
type LPCState struct {
	Supported         bool       `json:"supported"`
	Limit             LimitState `json:"limit"`
	FailsafePowerW    float64    `json:"failsafe_power_w"`
	FailsafeDurationS float64    `json:"failsafe_duration_s"`
	NominalMaxW       float64    `json:"nominal_max_w"`
	HeartbeatOK       bool       `json:"heartbeat_ok"`
}

// LPPState is the Controllable-System view of "Limitation of Power Production".
type LPPState struct {
	Supported         bool       `json:"supported"`
	Limit             LimitState `json:"limit"`
	FailsafePowerW    float64    `json:"failsafe_power_w"`
	FailsafeDurationS float64    `json:"failsafe_duration_s"`
	NominalMaxW       float64    `json:"nominal_max_w"`
	HeartbeatOK       bool       `json:"heartbeat_ok"`
}

// MPCState is "Monitoring of Power Consumption" measurement data.
type MPCState struct {
	Supported        bool      `json:"supported"`
	PowerW           *float64  `json:"power_w,omitempty"`
	PowerPerPhaseW   []float64 `json:"power_per_phase_w,omitempty"`
	EnergyConsumedWh *float64  `json:"energy_consumed_wh,omitempty"`
	EnergyProducedWh *float64  `json:"energy_produced_wh,omitempty"`
	CurrentPerPhaseA []float64 `json:"current_per_phase_a,omitempty"`
	VoltagePerPhaseV []float64 `json:"voltage_per_phase_v,omitempty"`
	FrequencyHz      *float64  `json:"frequency_hz,omitempty"`
}

// BatteryState is VABD data (eebus-go equivalent of Monitoring of Battery / MOB).
type BatteryState struct {
	Supported          bool     `json:"supported"`
	PowerW             *float64 `json:"power_w,omitempty"`
	StateOfChargePct   *float64 `json:"state_of_charge_pct,omitempty"`
	EnergyChargedWh    *float64 `json:"energy_charged_wh,omitempty"`
	EnergyDischargedWh *float64 `json:"energy_discharged_wh,omitempty"`
}

// InverterState is VAPD data (eebus-go equivalent of Monitoring of Inverter / MOI).
type InverterState struct {
	Supported         bool     `json:"supported"`
	PowerW            *float64 `json:"power_w,omitempty"`
	PVYieldTotalWh    *float64 `json:"pv_yield_total_wh,omitempty"`
	PowerNominalPeakW *float64 `json:"power_nominal_peak_w,omitempty"`
}

// InverterControl reflects the Energy-Guard control of the inverter.
//
// The *Target* values are staged by the user (e.g. via a Number entity) and can
// be set at any time, even while no inverter is connected. The *Limit* values
// are what was actually last commanded to the inverter, and *Active* tracks
// whether that command is currently applied.
type InverterControl struct {
	Supported          bool    `json:"supported"`
	DurationS          float64 `json:"duration_s"`
	ConsumptionTargetW float64 `json:"consumption_target_w"`
	ConsumptionLimitW  float64 `json:"consumption_limit_w"`
	ConsumptionActive  bool    `json:"consumption_active"`
	ProductionTargetW  float64 `json:"production_target_w"`
	ProductionLimitW   float64 `json:"production_limit_w"`
	ProductionActive   bool    `json:"production_active"`
}

// State is the complete snapshot serialised to the integration.
type State struct {
	Service         ServiceInfo     `json:"service"`
	Connected       bool            `json:"connected"`
	Connections     []Connection    `json:"connections"`
	LPC             LPCState        `json:"lpc"`
	LPP             LPPState        `json:"lpp"`
	MPC             MPCState        `json:"mpc"`
	Battery         BatteryState    `json:"battery"`
	Inverter        InverterState   `json:"inverter"`
	InverterControl InverterControl `json:"inverter_control"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// Store is a concurrency-safe container around State with a simple fan-out
// notification mechanism for WebSocket subscribers.
type Store struct {
	mu          sync.RWMutex
	state       State
	subscribers map[int]chan State
	nextSub     int
}

// New creates an empty Store.
func New() *Store {
	return &Store{
		subscribers: make(map[int]chan State),
	}
}

// Snapshot returns a copy of the current state.
func (s *Store) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// Update mutates the state under lock and notifies subscribers. The mutator must
// not block.
func (s *Store) Update(mutate func(*State)) {
	s.mu.Lock()
	mutate(&s.state)
	s.state.UpdatedAt = time.Now()
	snapshot := s.state
	subs := make([]chan State, 0, len(s.subscribers))
	for _, ch := range s.subscribers {
		subs = append(subs, ch)
	}
	s.mu.Unlock()

	for _, ch := range subs {
		// Non-blocking publish with a coalescing drop: each subscriber channel is
		// buffered (size 1). If it is full the subscriber is already behind and
		// will pick up the newer full snapshot on its next read, so dropping the
		// intermediate state is safe.
		select {
		case ch <- snapshot:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- snapshot:
			default:
			}
		}
	}
}

// Subscribe registers a new subscriber and returns its channel plus an
// unsubscribe function.
func (s *Store) Subscribe() (<-chan State, func()) {
	s.mu.Lock()
	id := s.nextSub
	s.nextSub++
	ch := make(chan State, 1)
	s.subscribers[id] = ch
	// seed with the current snapshot so a new client gets state immediately
	ch <- s.state
	s.mu.Unlock()

	return ch, func() {
		s.mu.Lock()
		if existing, ok := s.subscribers[id]; ok {
			delete(s.subscribers, id)
			close(existing)
		}
		s.mu.Unlock()
	}
}
