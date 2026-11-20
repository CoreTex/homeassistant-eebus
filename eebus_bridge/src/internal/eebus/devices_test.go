package eebus

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/config"
	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"

	spineapi "github.com/enbility/spine-go/api"
)

const (
	skiInverter = "1111111111111111111111111111111111111111"
	skiWallbox1 = "2222222222222222222222222222222222222222"
	skiWallbox2 = "3333333333333333333333333333333333333333"
)

func newTestManager(t *testing.T, cfg config.Config) *Manager {
	t.Helper()
	if cfg.DataDir == "" {
		cfg.DataDir = t.TempDir()
	}
	return &Manager{
		cfg:           cfg,
		store:         store.New(),
		logger:        NewLogger("error"),
		conns:         make(map[string]*store.Connection),
		primarySKI:    cfg.InverterSKI,
		primaryWarned: make(map[string]bool),
		egEntities:    map[LimitKind]map[string]spineapi.EntityRemoteInterface{KindLPC: {}, KindLPP: {}},
		evEntities:    make(map[string]spineapi.EntityRemoteInterface),
	}
}

func TestIsPrimaryPinnedByInverterSKI(t *testing.T) {
	m := newTestManager(t, config.Config{InverterSKI: skiInverter})
	if m.isPrimary(skiWallbox1) {
		t.Error("wallbox must not be primary when inverter_ski is set")
	}
	if !m.isPrimary(skiInverter) {
		t.Error("configured inverter must be primary")
	}
}

func TestIsPrimaryFirstReporterSticks(t *testing.T) {
	m := newTestManager(t, config.Config{})
	if !m.isPrimary(skiInverter) {
		t.Fatal("first reporter should become primary")
	}
	if m.isPrimary(skiWallbox1) || m.isPrimary(skiWallbox2) {
		t.Error("later reporters must not replace the primary")
	}
	if !m.isPrimary(skiInverter) {
		t.Error("primary must stay stable")
	}
	if got := m.store.Snapshot().PrimarySKI; got != skiInverter {
		t.Errorf("PrimarySKI = %q, want %q", got, skiInverter)
	}
	if m.isPrimary("") {
		t.Error("empty SKI is never primary")
	}
}

func TestPersistedTrustRoundTrip(t *testing.T) {
	m := newTestManager(t, config.Config{})
	if got := m.loadPersistedTrust(); len(got) != 0 {
		t.Fatalf("expected no persisted trust, got %v", got)
	}

	m.setPersistedTrust(skiWallbox2, true)
	m.setPersistedTrust(skiWallbox1, true)
	m.setPersistedTrust(skiWallbox1, true) // idempotent
	if got := m.loadPersistedTrust(); !slices.Equal(got, []string{skiWallbox1, skiWallbox2}) {
		t.Fatalf("after trust: %v", got)
	}

	m.setPersistedTrust(skiWallbox2, false)
	if got := m.loadPersistedTrust(); !slices.Equal(got, []string{skiWallbox1}) {
		t.Fatalf("after forget: %v", got)
	}
}

func TestPersistedTrustIgnoresGarbage(t *testing.T) {
	m := newTestManager(t, config.Config{})
	path := filepath.Join(m.cfg.DataDir, trustFileName)
	if err := os.WriteFile(path, []byte(`["`+skiWallbox1+`","nope"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := m.loadPersistedTrust(); !slices.Equal(got, []string{skiWallbox1}) {
		t.Errorf("got %v, want only the valid SKI", got)
	}
	if err := os.WriteFile(path, []byte(`{broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := m.loadPersistedTrust(); got != nil {
		t.Errorf("corrupt file should yield nil, got %v", got)
	}
}

func TestSyncConnectionsFlags(t *testing.T) {
	m := newTestManager(t, config.Config{InverterSKI: skiInverter})
	m.upsertConn(skiWallbox1, func(c *store.Connection) { c.Connected = true })
	m.upsertConn(skiInverter, func(c *store.Connection) { c.Connected = true })

	conns := m.store.Snapshot().Connections
	if len(conns) != 2 || conns[0].SKI != skiInverter || conns[1].SKI != skiWallbox1 {
		t.Fatalf("connections not sorted by SKI: %+v", conns)
	}
	if !conns[0].Primary || conns[1].Primary {
		t.Errorf("primary flag wrong: %+v", conns)
	}
}

type spineEntity = spineapi.EntityRemoteInterface
