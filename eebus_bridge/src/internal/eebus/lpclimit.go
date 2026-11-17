package eebus

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"

	ucapi "github.com/enbility/eebus-go/usecases/api"
)

// Incoming consumption limit (the bridge as Controllable System): expiry and
// simulation.
//
// eebus-go stores a limit's duration but does not end the limit when it runs
// out. Per LPC the Controllable System returns to "unlimited" after the
// duration, so the bridge arms a timer and deactivates the limit itself -
// otherwise a §14a share would stay in force after the grid operator's limit
// ended.

type lpcExpiry struct {
	mu        sync.Mutex
	key       string // fingerprint of the limit the timer was armed for
	timer     *time.Timer
	expiresAt time.Time
	simulated bool
}

func limitKey(l ucapi.LoadLimit) string {
	return fmt.Sprintf("%t|%g|%d", l.IsActive, l.Value, l.Duration)
}

// trackLPCExpiry (re)arms the expiry timer for the current limit and returns
// its expiry time (nil without an expiring limit). A limit with an absolute
// end time reports a shrinking duration on every read, which re-arms the timer
// for the same moment; a relative one keeps its key and is armed once.
func (m *Manager) trackLPCExpiry(l ucapi.LoadLimit) *time.Time {
	e := &m.lpcExpiry
	e.mu.Lock()
	defer e.mu.Unlock()

	if !l.IsActive || l.Duration <= 0 {
		if e.timer != nil {
			e.timer.Stop()
			e.timer = nil
		}
		e.key = ""
		if !l.IsActive {
			e.simulated = false
		}
		return nil
	}
	key := limitKey(l)
	if key != e.key || e.timer == nil {
		if e.timer != nil {
			e.timer.Stop()
		}
		e.key = key
		e.expiresAt = time.Now().Add(l.Duration)
		e.timer = time.AfterFunc(l.Duration, func() { m.expireLPC(key) })
	}
	t := e.expiresAt
	return &t
}

// expireLPC ends the limit the timer was armed for, unless it changed since.
func (m *Manager) expireLPC(key string) {
	e := &m.lpcExpiry
	e.mu.Lock()
	if e.key != key {
		e.mu.Unlock()
		return
	}
	e.key, e.timer, e.simulated = "", nil, false
	e.mu.Unlock()

	if m.csLPC == nil {
		return
	}
	current, err := m.csLPC.ConsumptionLimit()
	if err != nil || limitKey(current) != key {
		return
	}
	m.logger.Infof("consumption limit of %.0f W expired after %s", current.Value, current.Duration)
	if err := m.csLPC.SetConsumptionLimit(ucapi.LoadLimit{
		Value:        current.Value,
		IsActive:     false,
		IsChangeable: current.IsChangeable,
	}); err != nil {
		m.logger.Errorf("ending expired consumption limit: %v", err)
	}
	m.refreshLPC()
}

func (m *Manager) lpcSimulated() bool {
	m.lpcExpiry.mu.Lock()
	defer m.lpcExpiry.mu.Unlock()
	return m.lpcExpiry.simulated
}

// SimulateGridLimit sets (or ends) a simulated incoming §14a consumption
// limit, as if the grid operator's control box had sent it. It runs through
// the normal path (state, distribution, expiry). For safety it is refused
// while a real control box is connected or a real limit is active, so it can
// never override or lift a genuine grid operator limit.
func (m *Manager) SimulateGridLimit(valueW float64, active bool, durationS float64) error {
	if !m.cfg.EnableLPCSimulation {
		return errors.New("simulation is disabled (enable_lpc_simulation)")
	}
	if m.csLPC == nil {
		return errors.New("lpc is disabled (enable_lpc)")
	}
	if valueW < 0 || durationS < 0 {
		return errors.New("value and duration must be >= 0")
	}
	simulated := m.lpcSimulated()

	if !active {
		if !simulated {
			return errors.New("no simulated limit is active")
		}
	} else {
		if src := m.connectedLPCSource(); src != "" {
			return fmt.Errorf("a real control box (%s) is connected; simulation refused", src)
		}
		if current, err := m.csLPC.ConsumptionLimit(); err == nil && current.IsActive && !simulated {
			return errors.New("a real consumption limit is active; simulation refused")
		}
	}

	if err := m.csLPC.SetConsumptionLimit(ucapi.LoadLimit{
		Value:        valueW,
		IsActive:     active,
		IsChangeable: true,
		Duration:     secondsToDuration(durationS),
	}); err != nil {
		return err
	}
	m.lpcExpiry.mu.Lock()
	m.lpcExpiry.simulated = active
	m.lpcExpiry.mu.Unlock()
	if active {
		m.logger.Infof("SIMULATION: grid operator consumption limit %.0f W for %.0f s", valueW, durationS)
	} else {
		m.logger.Infof("SIMULATION: grid operator consumption limit ended")
	}
	m.refreshLPC()
	return nil
}

// connectedLPCSource returns the SKI of a connected device that sends
// consumption limits to the bridge (a control box), if any.
func (m *Manager) connectedLPCSource() string {
	snap := m.store.Snapshot()
	for _, c := range snap.Connections {
		if c.Connected && snap.Devices[c.SKI].HasUseCase(ucLPCSource) {
			return c.SKI
		}
	}
	return ""
}

// lpcState builds the store view of the incoming limit.
func (m *Manager) lpcLimitState(l ucapi.LoadLimit) store.LimitState {
	ls := toLimitState(l)
	ls.ExpiresAt = m.trackLPCExpiry(l)
	return ls
}
