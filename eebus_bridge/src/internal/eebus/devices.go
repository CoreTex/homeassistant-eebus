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
// mpc/battery/inverter blocks. With inverter_ski configured only that device is
// primary. Without it, the first device that reports measurements is chosen
// and kept for the lifetime of the process; data from other devices is still
// available per device under State.Devices.
func (m *Manager) isPrimary(ski string) bool {
	if ski == "" {
		return false
	}
	m.mu.Lock()
	chosen := false
	if m.primarySKI == "" {
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
	if warn {
		m.logger.Infof("device %s reports measurements; kept separate from primary device %s", ski, current)
	}
	return primary
}

// --- Energy Guard target (the device inverter_control limits are written to) ---

// acceptEGTarget decides whether entity may become the Energy-Guard target.
// With inverter_ski configured only that device qualifies. Without it the first
// device announcing LPC/LPP support wins; another device (e.g. a wallbox that
// is itself an LPC Controllable System) never silently replaces it.
func (m *Manager) acceptEGTarget(entity spineapi.EntityRemoteInterface) bool {
	ski := entitySKI(entity)
	if ski == "" {
		return false
	}
	if m.cfg.InverterSKI != "" {
		if ski != m.cfg.InverterSKI {
			m.logger.Infof("ignoring Energy-Guard candidate %s: inverter_ski is %s", ski, m.cfg.InverterSKI)
			return false
		}
		return true
	}
	m.mu.Lock()
	current := entitySKI(m.egEntity)
	m.mu.Unlock()
	if current != "" && current != ski {
		m.logger.Infof("ignoring Energy-Guard candidate %s: already controlling %s (set inverter_ski to choose)", ski, current)
		return false
	}
	return true
}

// setEGTarget stores entity as the Energy-Guard target.
func (m *Manager) setEGTarget(entity spineapi.EntityRemoteInterface) {
	m.mu.Lock()
	m.egEntity = entity
	m.mu.Unlock()
	m.syncConnections()
}

// isEGTarget reports whether entity belongs to the current Energy-Guard target.
func (m *Manager) isEGTarget(entity spineapi.EntityRemoteInterface) bool {
	ski := entitySKI(entity)
	m.mu.Lock()
	defer m.mu.Unlock()
	return ski != "" && ski == entitySKI(m.egEntity)
}

// clearEGTargetFor drops the Energy-Guard target when its device disconnects,
// so a reconnect (with fresh SPINE entities) is re-discovered cleanly.
func (m *Manager) clearEGTargetFor(ski string) {
	m.mu.Lock()
	if entitySKI(m.egEntity) == ski {
		m.egEntity = nil
	}
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
