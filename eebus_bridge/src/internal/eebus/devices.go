package eebus

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	spineapi "github.com/enbility/spine-go/api"
)

// trustFileName is where SKIs trusted at runtime (trust_device service, or
// auto-accept) are persisted, so pairings survive an add-on restart.
const trustFileName = "trusted_skis.json"

// --- primary device (mirrored into the top-level measurement blocks) ---

// isPrimary reports whether measurements from ski feed the top-level
// mpc/battery/inverter blocks, i.e. whether ski is the leading inverter. With
// inverter_ski configured only that device leads. Without it, the first
// eligible device (inverter or unclassified, never a wallbox/heat pump/meter)
// that reports measurements is chosen; data from all devices stays available
// per device under State.Devices.
func (m *Manager) isPrimary(ski string) bool {
	if ski == "" {
		return false
	}
	eligible := primaryEligible(m.categoryOf(ski))
	m.mu.Lock()
	chosen := false
	if m.primarySKI == "" && eligible {
		m.primarySKI = ski
		chosen = true
	}
	primary := m.primarySKI == ski
	warn := !primary && !m.primaryWarned[ski]
	if warn {
		m.primaryWarned[ski] = true
	}
	current := m.primarySKI
	m.mu.Unlock()

	if chosen {
		m.logger.Infof("using %s as primary device for mpc/battery/inverter data (set inverter_ski to pin it)", ski)
		m.syncConnections()
	}
	if warn && current != "" {
		m.logger.Infof("device %s reports measurements; kept separate from leading inverter %s", ski, current)
	}
	return primary
}

// forgetEntities drops the SPINE entities of a disconnected device; they are
// re-discovered (fresh objects) on reconnect, where limits are re-applied.
func (m *Manager) forgetEntities(ski string) {
	m.mu.Lock()
	for _, ents := range m.egEntities {
		delete(ents, ski)
	}
	delete(m.evEntities, ski)
	delete(m.flexEntities, ski)
	delete(m.lpcControlled, ski)
	delete(m.lpcRetries, ski)
	m.mu.Unlock()
}

func entitySKI(entity spineapi.EntityRemoteInterface) string {
	if entity == nil || entity.Device() == nil {
		return ""
	}
	return entity.Device().Ski()
}

// --- persisted runtime trust ---

func (m *Manager) trustFilePath() string {
	return filepath.Join(m.cfg.DataDir, trustFileName)
}

// loadPersistedTrust returns the SKIs trusted at runtime in earlier runs.
func (m *Manager) loadPersistedTrust() []string {
	data, err := os.ReadFile(m.trustFilePath())
	if err != nil {
		if !os.IsNotExist(err) {
			m.logger.Errorf("reading %s: %v", m.trustFilePath(), err)
		}
		return nil
	}
	var skis []string
	if err := json.Unmarshal(data, &skis); err != nil {
		m.logger.Errorf("parsing %s: %v", m.trustFilePath(), err)
		return nil
	}
	out := make([]string, 0, len(skis))
	for _, s := range skis {
		if s = normaliseSKI(s); validSKI(s) {
			out = append(out, s)
		}
	}
	return out
}

// setPersistedTrust adds (trusted=true) or removes ski from the trust file.
func (m *Manager) setPersistedTrust(ski string, trusted bool) {
	m.trustMu.Lock()
	defer m.trustMu.Unlock()

	skis := m.loadPersistedTrust()
	has := slices.Contains(skis, ski)
	switch {
	case trusted && !has:
		skis = append(skis, ski)
	case !trusted && has:
		skis = slices.DeleteFunc(skis, func(s string) bool { return s == ski })
	default:
		return
	}
	slices.Sort(skis)
	data, err := json.MarshalIndent(skis, "", "  ")
	if err != nil {
		m.logger.Errorf("encoding trusted SKIs: %v", err)
		return
	}
	if err := os.MkdirAll(m.cfg.DataDir, 0o755); err != nil {
		m.logger.Errorf("creating %s: %v", m.cfg.DataDir, err)
		return
	}
	tmp := m.trustFilePath() + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		m.logger.Errorf("writing %s: %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, m.trustFilePath()); err != nil {
		m.logger.Errorf("replacing %s: %v", m.trustFilePath(), err)
	}
}
