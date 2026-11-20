package eebus

import (
	"math"
	"slices"
	"time"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"
)

// §14a EnWG distribution (opt-in via distribute_lpc).
//
// When the grid operator's control box sends an active LPC consumption limit
// to the bridge (the bridge is the Controllable System of the whole home), the
// bridge, as the home's energy manager, shares that total limit among the
// controllable consumers it knows:
//
//   - wallboxes and heat pumps,
//   - inverters with a battery (LPC limits their grid draw, i.e. charging the
//     battery from the grid) when distribute_lpc_inverters is enabled,
//
// via LPC where the device accepts it, otherwise (wallboxes) by capping the
// charging current via OPEV.
//
// The limit is shared by demand (max-min fairness): a device whose consumption
// is measured gets about its current demand plus a ramp-up reserve, devices
// without a measurement share the rest. As a device draws more, its share grows
// with every re-evaluation. The sum of all shares never exceeds the limit.
//
// The share is combined with any user limit (the lower one wins) and removed
// when the incoming limit ends. This lives in the bridge (not in Home
// Assistant) so it keeps working while Home Assistant restarts.

const (
	nominalPhaseVoltage = 230.0

	// A measured consumer is granted demand*demandHeadroom + rampReserveW, so a
	// device at its share can grow by >25 % + 500 W per re-evaluation.
	demandHeadroom = 1.25
	rampReserveW   = 500.0
	// Shares are rounded down to this step and only rewritten when they move
	// by at least shareHysteresisW, to keep traffic on the bus low.
	shareStepW       = 100.0
	shareHysteresisW = 200.0
	// Measurements re-trigger the distribution at most this often.
	redistributeEvery = 10 * time.Second
)

// distributionConsumer describes a device that can take a share.
type distributionConsumer struct {
	SKI    string
	ViaLPC bool // limit via LPC; otherwise via OPEV current cap
	Phases uint
	// DemandW is the measured consumption (nil = unknown).
	DemandW *float64
	ShareW  float64
}

// allocateShares splits limitW among the consumers by max-min fairness.
// Consumers with a known demand are capped at demand*headroom+reserve; the
// remainder goes to the others. If every consumer is capped, the leftover is
// spread equally so no part of the limit is wasted.
func allocateShares(limitW float64, consumers []distributionConsumer) {
	n := len(consumers)
	if n == 0 {
		return
	}
	caps := make([]float64, n)
	for i, c := range consumers {
		caps[i] = math.Inf(1)
		if c.DemandW != nil {
			caps[i] = math.Max(0, *c.DemandW)*demandHeadroom + rampReserveW
		}
		consumers[i].ShareW = 0
	}

	remaining := limitW
	open := make([]int, 0, n)
	for i := range consumers {
		open = append(open, i)
	}
	// water filling: give everyone an equal slice, cap, repeat with the rest
	for len(open) > 0 && remaining > 1e-9 {
		slice := remaining / float64(len(open))
		next := open[:0:0]
		for _, i := range open {
			grant := math.Min(slice, caps[i]-consumers[i].ShareW)
			consumers[i].ShareW += grant
			remaining -= grant
			if consumers[i].ShareW < caps[i]-1e-9 {
				next = append(next, i)
			}
		}
		if len(next) == len(open) {
			break // nobody was capped: the equal slices used up the limit
		}
		open = next
	}
	if remaining > 1e-9 { // all capped: spread the leftover
		for i := range consumers {
			consumers[i].ShareW += remaining / float64(n)
		}
	}
	for i := range consumers {
		consumers[i].ShareW = math.Floor(consumers[i].ShareW/shareStepW) * shareStepW
	}
}

// consumerDemand is the measured consumption of a device, if any (W, >= 0).
func consumerDemand(d store.DeviceData) *float64 {
	switch {
	case d.EV != nil && d.EV.PowerW != nil:
		return ptr(math.Max(0, *d.EV.PowerW))
	case d.MPC != nil && d.MPC.PowerW != nil: // positive = consumption
		return ptr(math.Max(0, *d.MPC.PowerW))
	case d.InverterAC != nil && d.InverterAC.PowerW != nil: // positive = production
		return ptr(math.Max(0, -*d.InverterAC.PowerW))
	}
	return nil
}

// planDistribution picks the consumers and computes their shares. It is a
// pure function of the state so it can be tested without EEBUS devices.
func planDistribution(s store.State, lpcCapable, opevCapable map[string]bool, includeInverters bool) []distributionConsumer {
	if !s.LPC.Limit.Active {
		return nil
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
		if !connected[ski] {
			continue
		}
		eligible := d.Category == store.CategoryHeatPump || d.Category == store.CategoryWallbox ||
			(includeInverters && d.Category == store.CategoryInverter)
		if !eligible {
			continue
		}
		c := distributionConsumer{SKI: ski, Phases: 3, DemandW: consumerDemand(d)}
		if d.EV != nil && d.EV.PhasesConnected != nil && *d.EV.PhasesConnected > 0 {
			c.Phases = *d.EV.PhasesConnected
		}
		switch {
		case lpcCapable[ski]:
			c.ViaLPC = true
		case opevCapable[ski] && d.Category == store.CategoryWallbox:
		default:
			continue
		}
		consumers = append(consumers, c)
	}
	allocateShares(s.LPC.Limit.ValueW, consumers)
	return consumers
}

// shareToCurrent converts a power share into a per-phase current (floored to
// whole amperes, never negative).
func shareToCurrent(shareW float64, phases uint) float64 {
	if phases == 0 {
		phases = 3
	}
	return math.Max(0, math.Floor(shareW/(nominalPhaseVoltage*float64(phases))))
}

// keepShare reports whether an applied share is close enough to the new one
// to leave it unchanged.
func keepShare(old *float64, newW float64) bool {
	return old != nil && math.Abs(*old-newW) < shareHysteresisW
}

// scheduleDistribution re-evaluates the share-out asynchronously (it is
// triggered from EEBUS callbacks and writes to devices).
func (m *Manager) scheduleDistribution() {
	if !m.cfg.DistributeLPC {
		return
	}
	go m.distribute()
}

// redistributeOnMeasurement re-evaluates the shares after a power
// measurement changed, throttled, and only while a limit is being shared.
func (m *Manager) redistributeOnMeasurement() {
	if !m.cfg.DistributeLPC || !m.store.Snapshot().Distribution.Active {
		return
	}
	now := time.Now().UnixNano()
	last := m.lastDistribution.Load()
	if now-last < int64(redistributeEvery) || !m.lastDistribution.CompareAndSwap(last, now) {
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
	consumers := planDistribution(snap, lpcCapable, opevCapable, m.cfg.DistributeLPCInverters)
	inPlan := map[string]distributionConsumer{}
	for _, c := range consumers {
		inPlan[c.SKI] = c
	}

	shares := map[string]float64{}
	for ski, d := range snap.Devices {
		c, planned := inPlan[ski]
		// LPC share
		var oldShare *float64
		if d.Control != nil {
			oldShare = d.Control.DistributedLimitW
		}
		wantShare := planned && c.ViaLPC
		switch {
		case wantShare && keepShare(oldShare, c.ShareW):
			shares[ski] = *oldShare
		case wantShare || oldShare != nil:
			m.updateControl(ski, func(ctl *store.DeviceControl) {
				if wantShare {
					ctl.DistributedLimitW = ptr(c.ShareW)
				} else {
					ctl.DistributedLimitW = nil
				}
			})
			if wantShare {
				shares[ski] = c.ShareW
			}
			if err := m.applyLimit(KindLPC, ski, false); err != nil {
				m.logger.Errorf("§14a share for %s: %v", ski, err)
			}
		}
		// OPEV cap
		hadCap := d.EV != nil && d.EV.CapCurrentA != nil
		wantCap := planned && !c.ViaLPC
		if wantCap {
			shares[ski] = c.ShareW
		}
		if wantCap || hadCap {
			m.updateEV(ski, func(ev *store.EVState) {
				if wantCap {
					ev.CapCurrentA = ptr(shareToCurrent(c.ShareW, c.Phases))
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
		Devices: devices,
		Shares:  shares,
	}
	if snap.LPC.Limit.Active {
		state.LimitW = snap.LPC.Limit.ValueW
		if len(consumers) > 0 {
			state.ShareW = state.LimitW / float64(len(consumers))
		}
	}
	if !distributionEqual(snap.Distribution, state) {
		if state.Active {
			m.logger.Infof("§14a: sharing %.0f W: %v", state.LimitW, shares)
		}
		if state.Active && !snap.Distribution.Active && !m.cfg.DistributeLPCInverters {
			for ski, d := range snap.Devices {
				if d.Category == store.CategoryInverter && lpcCapable[ski] {
					m.logger.Infof("§14a: inverter %s not included (enable distribute_lpc_inverters if its storage is registered under §14a)", ski)
				}
			}
		}
		m.store.Update(func(s *store.State) { s.Distribution = state })
	}
}

func distributionEqual(a, b store.DistributionState) bool {
	if a.Enabled != b.Enabled || a.Active != b.Active || a.LimitW != b.LimitW ||
		a.ShareW != b.ShareW || !slices.Equal(a.Devices, b.Devices) || len(a.Shares) != len(b.Shares) {
		return false
	}
	for k, v := range a.Shares {
		if w, ok := b.Shares[k]; !ok || w != v {
			return false
		}
	}
	return true
}
