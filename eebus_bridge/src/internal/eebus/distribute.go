package eebus

import (
	"math"
	"slices"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"
)

// §14a EnWG distribution (opt-in via distribute_lpc).
//
// When the grid operator's control box sends an active LPC consumption limit
// to the bridge (the bridge is the Controllable System of the whole home), the
// bridge, as the home's energy manager, shares that total limit equally among
// the controllable consumers it knows:
//
//   - heat pumps and other appliances that accept LPC from us get an LPC limit,
//   - wallboxes that accept LPC get an LPC limit, otherwise their charging
//     current is capped via OPEV (watts -> amperes per connected phase).
//
// The share is combined with any user limit (the lower one wins) and removed
// again when the incoming limit ends. Inverters are not part of the share-out.
// This lives in the bridge (not in Home Assistant) so it keeps working while
// Home Assistant restarts.

const nominalPhaseVoltage = 230.0

// distributionConsumer describes a device that can take a share.
type distributionConsumer struct {
	SKI    string
	ViaLPC bool // limit via LPC; otherwise via OPEV current cap
	Phases uint
}

// planDistribution picks the consumers and computes the equal share. It is a
// pure function of the state so it can be tested without EEBUS devices.
func planDistribution(s store.State, lpcCapable map[string]bool, opevCapable map[string]bool) ([]distributionConsumer, float64) {
	if !s.LPC.Limit.Active {
		return nil, 0
	}
	connected := map[string]bool{}
	for _, c := range s.Connections {
		connected[c.SKI] = c.Connected
	}
	skis := make([]string, 0, len(s.Devices))
	for ski := range s.Devices {
		skis = append(skis, ski)
	}
	slices.Sort(skis)

	var consumers []distributionConsumer
	for _, ski := range skis {
		d := s.Devices[ski]
		if !connected[ski] || (d.Category != store.CategoryHeatPump && d.Category != store.CategoryWallbox) {
			continue
		}
		c := distributionConsumer{SKI: ski, Phases: 3}
		if d.EV != nil && d.EV.PhasesConnected != nil && *d.EV.PhasesConnected > 0 {
			c.Phases = *d.EV.PhasesConnected
		}
		switch {
		case lpcCapable[ski]:
			c.ViaLPC = true
		case opevCapable[ski]:
		default:
			continue
		}
		consumers = append(consumers, c)
	}
	if len(consumers) == 0 {
		return nil, 0
	}
	return consumers, s.LPC.Limit.ValueW / float64(len(consumers))
}

// shareToCurrent converts a power share into a per-phase current (floored to
// whole amperes, never negative).
func shareToCurrent(shareW float64, phases uint) float64 {
	if phases == 0 {
		phases = 3
	}
	return math.Max(0, math.Floor(shareW/(nominalPhaseVoltage*float64(phases))))
}

// scheduleDistribution re-evaluates the share-out asynchronously (it is
// triggered from EEBUS callbacks and writes to devices).
func (m *Manager) scheduleDistribution() {
	if !m.cfg.DistributeLPC {
		return
	}
	go m.distribute()
}

func (m *Manager) distribute() {
	m.distMu.Lock()
	defer m.distMu.Unlock()

	m.mu.Lock()
	lpcCapable := map[string]bool{}
	for ski, e := range m.egEntities[KindLPC] {
		lpcCapable[ski] = e != nil && m.egLPC != nil
	}
	opevCapable := map[string]bool{}
	for ski, e := range m.evEntities {
		opevCapable[ski] = e != nil && m.opev != nil
	}
	m.mu.Unlock()

	snap := m.store.Snapshot()
	consumers, share := planDistribution(snap, lpcCapable, opevCapable)
	inPlan := map[string]distributionConsumer{}
	for _, c := range consumers {
		inPlan[c.SKI] = c
	}

	for ski, d := range snap.Devices {
		c, planned := inPlan[ski]
		// LPC share
		hadShare := d.Control != nil && d.Control.DistributedLimitW != nil
		wantShare := planned && c.ViaLPC
		if wantShare || hadShare {
			m.updateControl(ski, func(ctl *store.DeviceControl) {
				if wantShare {
					ctl.DistributedLimitW = ptr(share)
				} else {
					ctl.DistributedLimitW = nil
				}
			})
			if err := m.applyLimit(KindLPC, ski, false); err != nil {
				m.logger.Errorf("§14a share for %s: %v", ski, err)
			}
		}
		// OPEV cap
		hadCap := d.EV != nil && d.EV.CapCurrentA != nil
		wantCap := planned && !c.ViaLPC
		if wantCap || hadCap {
			m.updateEV(ski, func(ev *store.EVState) {
				if wantCap {
					ev.CapCurrentA = ptr(shareToCurrent(share, c.Phases))
				} else {
					ev.CapCurrentA = nil
				}
			})
			if err := m.applyEV(ski, false); err != nil {
				m.logger.Errorf("§14a current cap for %s: %v", ski, err)
			}
		}
	}

	devices := make([]string, 0, len(consumers))
	for _, c := range consumers {
		devices = append(devices, c.SKI)
	}
	state := store.DistributionState{
		Enabled: true,
		Active:  len(consumers) > 0,
		LimitW:  snap.LPC.Limit.ValueW,
		ShareW:  share,
		Devices: devices,
	}
	if !snap.LPC.Limit.Active {
		state.LimitW = 0
	}
	if !slices.Equal(snap.Distribution.Devices, state.Devices) || snap.Distribution.Active != state.Active ||
		snap.Distribution.ShareW != state.ShareW || snap.Distribution.LimitW != state.LimitW || !snap.Distribution.Enabled {
		if state.Active {
			m.logger.Infof("§14a: sharing %.0f W across %d device(s), %.0f W each", state.LimitW, len(devices), share)
		}
		m.store.Update(func(s *store.State) { s.Distribution = state })
	}
}
