package eebus

import (
	"testing"
	"time"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/config"
	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"

	ucapi "github.com/enbility/eebus-go/usecases/api"
)

// newRealManager builds a Manager with a real eebus-go service (set up but
// not started, so nothing listens on the network).
func newRealManager(t *testing.T, mutate func(*config.Config)) *Manager {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.DeviceSerial = "test"
	cfg.EEBUSPort = 47150
	cfg.LogLevel = "error"
	cfg.EnableLPCSimulation = true
	if mutate != nil {
		mutate(&cfg)
	}
	m, err := NewManager(cfg, store.New(), "test")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestSimulateGridLimitLifecycle(t *testing.T) {
	m := newRealManager(t, nil)

	if err := m.SimulateGridLimit(0, false, 0); err == nil {
		t.Error("ending without an active simulation must fail")
	}

	if err := m.SimulateGridLimit(4200, true, 1); err != nil {
		t.Fatalf("simulate: %v", err)
	}
	lpc := m.store.Snapshot().LPC
	if !lpc.Limit.Active || lpc.Limit.ValueW != 4200 || !lpc.Simulated || lpc.Limit.ExpiresAt == nil {
		t.Fatalf("simulated limit not active: %+v", lpc)
	}

	// the 1 s limit expires on its own (eebus-go alone would keep it active)
	waitFor(t, "limit expiry", func() bool { return !m.store.Snapshot().LPC.Limit.Active })
	if m.store.Snapshot().LPC.Simulated {
		t.Error("simulated flag must clear when the limit ends")
	}
	if current, err := m.csLPC.ConsumptionLimit(); err != nil || current.IsActive {
		t.Errorf("eebus-go limit still active: %+v %v", current, err)
	}
}

func TestSimulateGridLimitManualEnd(t *testing.T) {
	m := newRealManager(t, nil)
	if err := m.SimulateGridLimit(3000, true, 0); err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if lpc := m.store.Snapshot().LPC; !lpc.Limit.Active || lpc.Limit.ExpiresAt != nil {
		t.Fatalf("unlimited duration expected: %+v", lpc)
	}
	if err := m.SimulateGridLimit(0, false, 0); err != nil {
		t.Fatalf("end: %v", err)
	}
	if lpc := m.store.Snapshot().LPC; lpc.Limit.Active || lpc.Simulated {
		t.Errorf("limit must be inactive: %+v", lpc)
	}
}

func TestSimulateGridLimitRefusesOverRealLimit(t *testing.T) {
	m := newRealManager(t, nil)
	// a real (non simulated) limit, as written by a control box
	if err := m.csLPC.SetConsumptionLimit(ucapi.LoadLimit{Value: 5000, IsActive: true, IsChangeable: true}); err != nil {
		t.Fatal(err)
	}
	m.refreshLPC()
	if err := m.SimulateGridLimit(4200, true, 60); err == nil {
		t.Fatal("simulation must not override a real limit")
	}
	if err := m.SimulateGridLimit(0, false, 0); err == nil {
		t.Fatal("simulation must not lift a real limit")
	}
	if current, _ := m.csLPC.ConsumptionLimit(); !current.IsActive || current.Value != 5000 {
		t.Errorf("real limit was changed: %+v", current)
	}
}

func TestSimulateGridLimitRefusesWithControlBox(t *testing.T) {
	m := newRealManager(t, nil)
	box := "5555555555555555555555555555555555555555"
	m.store.Update(func(s *store.State) {
		s.UpdateDevice(box, func(d *store.DeviceData) { d.UseCases = []string{ucLPCSource} })
	})
	m.upsertConn(box, func(c *store.Connection) { c.Connected = true })
	if err := m.SimulateGridLimit(4200, true, 60); err == nil {
		t.Fatal("simulation must be refused while a control box is connected")
	}
}

func TestSimulateGridLimitDisabled(t *testing.T) {
	m := newRealManager(t, func(c *config.Config) { c.EnableLPCSimulation = false })
	if err := m.SimulateGridLimit(4200, true, 60); err == nil {
		t.Fatal("simulation must be off by default")
	}
}

func TestTrackLPCExpiryRearm(t *testing.T) {
	m := newTestManager(t, config.Config{})
	l := ucapi.LoadLimit{Value: 4200, IsActive: true, Duration: time.Hour}
	first := m.trackLPCExpiry(l)
	if first == nil {
		t.Fatal("expiry expected")
	}
	// same relative limit read again: same expiry (not postponed)
	time.Sleep(10 * time.Millisecond)
	if again := m.trackLPCExpiry(l); !again.Equal(*first) {
		t.Errorf("re-reading must not postpone the expiry: %v vs %v", again, first)
	}
	// changed limit: re-armed
	l.Value = 3000
	if changed := m.trackLPCExpiry(l); changed.Equal(*first) {
		t.Error("a changed limit must re-arm the timer")
	}
	// inactive: disarmed
	if m.trackLPCExpiry(ucapi.LoadLimit{}) != nil || m.lpcExpiry.timer != nil {
		t.Error("inactive limit must disarm the timer")
	}
	m.expireLPC("stale") // no-op for a stale key
}
