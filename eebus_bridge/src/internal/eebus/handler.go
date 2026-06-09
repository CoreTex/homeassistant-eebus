package eebus

import (
	"slices"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"

	eebusapi "github.com/enbility/eebus-go/api"
	shipapi "github.com/enbility/ship-go/api"
)

// --- eebusapi.ServiceReaderInterface ---

func (m *Manager) RemoteServiceConnected(_ eebusapi.ServiceInterface, identity shipapi.ServiceIdentity) {
	m.logger.Infof("remote connected: %s", identity.SKI)
	m.upsertConn(identity.SKI, func(c *store.Connection) {
		c.Connected = true
		c.Trusted = true
	})
}

func (m *Manager) RemoteServiceDisconnected(_ eebusapi.ServiceInterface, identity shipapi.ServiceIdentity) {
	m.logger.Infof("remote disconnected: %s", identity.SKI)
	m.upsertConn(identity.SKI, func(c *store.Connection) {
		c.Connected = false
	})
}

func (m *Manager) VisibleRemoteMdnsServicesUpdated(_ eebusapi.ServiceInterface, entries []shipapi.RemoteMdnsService) {
	for _, e := range entries {
		if e.Ski == "" {
			continue
		}
		m.upsertConn(e.Ski, func(c *store.Connection) {
			if e.Name != "" {
				c.Name = e.Name
			}
			if e.Brand != "" {
				c.Brand = e.Brand
			}
			if e.Model != "" {
				c.Model = e.Model
			}
		})
	}
}

func (m *Manager) ServiceUpdated(identity shipapi.ServiceIdentity) {
	m.upsertConn(identity.SKI, func(c *store.Connection) {})
}

func (m *Manager) ServicePairingDetailUpdate(identity shipapi.ServiceIdentity, detail *shipapi.ConnectionStateDetail) {
	if detail == nil {
		return
	}
	m.logger.Debugf("pairing detail for %s: %v", identity.SKI, detail.State())
	if detail.State() == shipapi.ConnectionStateRemoteDeniedTrust {
		m.logger.Errorf("remote %s denied trust", identity.SKI)
	}
}

func (m *Manager) ServiceAutoTrusted(_ eebusapi.ServiceInterface, identity shipapi.ServiceIdentity) {
	m.logger.Infof("auto-trusted %s", identity.SKI)
	m.upsertConn(identity.SKI, func(c *store.Connection) { c.Trusted = true })
}

func (m *Manager) ServiceAutoTrustFailed(_ eebusapi.ServiceInterface, identity shipapi.ServiceIdentity, reason error) {
	m.logger.Errorf("auto-trust failed for %s: %v", identity.SKI, reason)
}

func (m *Manager) ServiceAutoTrustRemoved(_ eebusapi.ServiceInterface, identity shipapi.ServiceIdentity, reason string) {
	m.logger.Infof("auto-trust removed for %s: %s", identity.SKI, reason)
	m.upsertConn(identity.SKI, func(c *store.Connection) { c.Trusted = false })
}

// AllowWaitingForTrust decides whether an incoming pairing request may proceed.
// When auto-accept is enabled every remote is allowed; otherwise only remotes
// the user has explicitly pre-trusted (config or runtime) are allowed.
func (m *Manager) AllowWaitingForTrust(identity shipapi.ServiceIdentity) bool {
	if m.cfg.AutoAcceptTrust {
		return true
	}
	if slices.Contains(m.cfg.TrustedSKIs, identity.SKI) || identity.SKI == m.cfg.InverterSKI {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conns[identity.SKI]
	return ok && c.Trusted
}
