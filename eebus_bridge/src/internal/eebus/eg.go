package eebus

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"

	eebusapi "github.com/enbility/eebus-go/api"
	ucapi "github.com/enbility/eebus-go/usecases/api"
	eglpc "github.com/enbility/eebus-go/usecases/eg/lpc"
	eglpp "github.com/enbility/eebus-go/usecases/eg/lpp"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
)

// LimitKind selects consumption (LPC) or production (LPP) limits.
type LimitKind int

const (
	KindLPC LimitKind = iota
	KindLPP
)

func (k LimitKind) String() string {
	if k == KindLPP {
		return "production"
	}
	return "consumption"
}

var (
	errEGDisabled   = errors.New("energy guard control is disabled (enable_eg_control)")
	errNoInverter   = errors.New("no controllable inverter connected")
	errNotConnected = errors.New("device is not connected or does not accept this limit; it is stored and applied once the device connects")
)

// --- events: discover controllable devices (Energy Guard towards them) ---

func (m *Manager) onEgLPC(_ string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	m.onEG(KindLPC, device, entity, event)
}

func (m *Manager) onEgLPP(_ string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	m.onEG(KindLPP, device, entity, event)
}

func (m *Manager) onEG(kind LimitKind, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	ski := entitySKI(entity)
	if ski == "" {
		return
	}
	switch event {
	case eglpc.UseCaseSupportUpdate, eglpp.UseCaseSupportUpdate:
		if !slices.Contains(m.egScenarios(kind, entity), uint(1)) {
			return
		}
		m.mu.Lock()
		m.egEntities[kind][ski] = entity
		m.mu.Unlock()
		if kind == KindLPC {
			m.noteDevice(device, ucEgLPC)
		} else {
			m.noteDevice(device, ucEgLPP)
		}
		m.updateControl(ski, func(c *store.DeviceControl) {
			if kind == KindLPC {
				c.LPCSupported = true
			} else {
				c.LPPSupported = true
			}
		})
		m.logger.Infof("%s accepts %s limits from the bridge", ski, kind)
		m.seedLeadingTargets()
		m.syncConnections()
		m.mirrorLeading()
		if kind == KindLPC {
			m.takeLPCControl(ski)
		}
		// Re-apply a user or §14a limit after a (re)connect.
		if err := m.applyLimit(kind, ski, false); err != nil && !errors.Is(err, errNotConnected) {
			m.logger.Errorf("re-applying %s limit to %s: %v", kind, ski, err)
		}
		m.scheduleDistribution()
	default:
		m.readBackLimit(kind, ski, entity)
		if kind == KindLPC {
			m.takeLPCControl(ski) // retry once the limit data is there
		}
	}
}

func (m *Manager) egScenarios(kind LimitKind, entity spineapi.EntityRemoteInterface) []uint {
	if kind == KindLPC && m.egLPC != nil {
		return m.egLPC.AvailableScenariosForEntity(entity)
	}
	if kind == KindLPP && m.egLPP != nil {
		return m.egLPP.AvailableScenariosForEntity(entity)
	}
	return nil
}

// takeLPCControl sends a heat pump that offers OHPCF a consumption limit once
// after it connected.
//
// Heat pumps such as the Bosch CS5800i/CS6800i only follow an energy manager
// that actually controls them via LPC: a heat pump that gets heartbeats but
// never a limit stays in its init/failsafe state, shows a communication error
// (Bosch: 1240) and does not start on OHPCF. Without a user or §14a limit the
// bridge sends a deactivated limit; some heat pumps reject that right after
// connecting (#7), then an active limit at the nominal maximum is sent, which
// does not limit anything. Other devices are left alone, as they may be
// controlled by another energy manager.
//
// It is called on every LPC/OHPCF event, so a write that is not possible yet
// (limit descriptions not read) is retried with the next event.
func (m *Manager) takeLPCControl(ski string) {
	m.mu.Lock()
	_, flex := m.flexEntities[ski]
	entity, lpc := m.egEntities[KindLPC][ski]
	done := m.lpcControlled[ski]
	if flex && lpc && !done {
		m.lpcControlled[ski] = true
	}
	m.mu.Unlock()
	if !flex || !lpc || done || m.egLPC == nil {
		return
	}

	ctl := store.DeviceControl{}
	if d := m.store.Snapshot().Devices[ski]; d.Control != nil {
		ctl = *d.Control
	}
	var err error
	if _, active := effectiveLimit(ctl.LPCActive, ctl.LPCTargetW, ctl.DistributedLimitW); active || entity == nil {
		err = m.applyLimit(KindLPC, ski, true)
	} else {
		first, fallback := m.unlimitedLimits(entity, ctl)
		_, err = m.egLPC.WriteConsumptionLimit(entity, first, m.takeoverResultCB(ski, entity, fallback))
	}
	if err != nil {
		m.mu.Lock()
		delete(m.lpcControlled, ski)
		m.mu.Unlock()
		if errors.Is(err, eebusapi.ErrDataNotAvailable) {
			m.logger.Debugf("heat pump %s: limit not sent yet, waiting for its limit data", ski)
			return
		}
		m.logger.Errorf("sending the consumption limit to %s: %v", ski, err)
		return
	}
	m.logger.Infof("heat pump %s: sent the consumption limit (bridge is its energy manager)", ski)
}

// unlimitedLimits returns the "no limit" for a heat pump the bridge manages
// and a fallback: an active limit at the nominal maximum first, as Bosch heat
// pumps only leave their failsafe state (error 1240, "communication to the
// energy manager lost") on an active limit (#7); a deactivated limit as
// fallback. Without a known nominal maximum only the deactivated one is used.
func (m *Manager) unlimitedLimits(entity spineapi.EntityRemoteInterface, ctl store.DeviceControl) (ucapi.LoadLimit, *ucapi.LoadLimit) {
	off := ucapi.LoadLimit{Value: m.lpcNominal(entity, ctl), IsChangeable: true}
	if nominal := m.lpcNominal(entity, ctl); nominal > 0 {
		return ucapi.LoadLimit{Value: nominal, IsActive: true, IsChangeable: true}, &off
	}
	return off, nil
}

// takeoverResultCB sends the fallback limit when the heat pump rejects the
// first one. If that is rejected too (some heat pumps reject every write for
// a while after connecting, #7), the takeover is retried later with a
// growing delay.
func (m *Manager) takeoverResultCB(ski string, entity spineapi.EntityRemoteInterface, fallback *ucapi.LoadLimit) func(model.ResultDataType, model.MsgCounterType) {
	return func(r model.ResultDataType, _ model.MsgCounterType) {
		if r.ErrorNumber == nil || *r.ErrorNumber == model.ErrorNumberTypeNoError {
			m.mu.Lock()
			delete(m.lpcRetries, ski)
			m.mu.Unlock()
			return
		}
		if fallback == nil {
			m.logger.Infof("heat pump %s rejected the consumption limit (code=%v)", ski, *r.ErrorNumber)
			m.retryTakeover(ski, entity)
			return
		}
		m.logger.Infof("heat pump %s rejected the limit (code=%v), sending active=%v %.0f W instead", ski, *r.ErrorNumber, fallback.IsActive, fallback.Value)
		limit := *fallback
		go func() { // not from within the result handler
			if _, err := m.egLPC.WriteConsumptionLimit(entity, limit, m.takeoverResultCB(ski, entity, nil)); err != nil {
				m.logger.Errorf("sending the consumption limit to %s: %v", ski, err)
				m.retryTakeover(ski, entity)
			}
		}()
	}
}

// maxTakeoverRetries limits the retries: 15 s, 30 s, 1, 2, 4 and 8 min.
const maxTakeoverRetries = 6

// takeoverDelay is the wait before retry n (1-based); a variable for tests.
var takeoverDelay = func(n int) time.Duration { return 15 * time.Second << (n - 1) }

// retryTakeover schedules another takeLPCControl while the heat pump stays
// connected.
func (m *Manager) retryTakeover(ski string, entity spineapi.EntityRemoteInterface) {
	m.mu.Lock()
	n := m.lpcRetries[ski] + 1
	if n > maxTakeoverRetries || m.egEntities[KindLPC][ski] != entity {
		m.mu.Unlock()
		if n > maxTakeoverRetries {
			m.logger.Errorf("heat pump %s keeps rejecting the consumption limit; giving up until it reconnects", ski)
		}
		return
	}
	m.lpcRetries[ski] = n
	m.mu.Unlock()
	d := takeoverDelay(n)
	m.logger.Infof("heat pump %s: retrying the consumption limit in %s", ski, d)
	time.AfterFunc(d, func() {
		m.mu.Lock()
		current := m.egEntities[KindLPC][ski] == entity
		if current {
			delete(m.lpcControlled, ski)
		}
		m.mu.Unlock()
		if current {
			m.takeLPCControl(ski)
		}
	})
}

// managedHeatPump reports whether the bridge acts as energy manager of a heat
// pump (it offers OHPCF and accepts LPC limits from the bridge).
func (m *Manager) managedHeatPump(ski string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, flex := m.flexEntities[ski]
	_, lpc := m.egEntities[KindLPC][ski]
	return flex && lpc
}

// lpcNominal returns the nominal maximum consumption of a device, 0 if unknown.
func (m *Manager) lpcNominal(entity spineapi.EntityRemoteInterface, ctl store.DeviceControl) float64 {
	if entity != nil && m.egLPC != nil {
		if v, err := m.egLPC.ConsumptionNominalMax(entity); err == nil && v > 0 {
			return v
		}
	}
	if ctl.LPCNominalMaxW != nil {
		return *ctl.LPCNominalMaxW
	}
	return 0
}

// readBackLimit stores the limit the device itself reports.
func (m *Manager) readBackLimit(kind LimitKind, ski string, entity spineapi.EntityRemoteInterface) {
	var (
		l   ucapi.LoadLimit
		err error
	)
	var nominal, failsafeW, failsafeS *float64
	if kind == KindLPC && m.egLPC != nil {
		l, err = m.egLPC.ConsumptionLimit(entity)
		if v, nerr := m.egLPC.ConsumptionNominalMax(entity); nerr == nil {
			nominal = ptr(v)
		}
		if v, ferr := m.egLPC.FailsafeConsumptionActivePowerLimit(entity); ferr == nil {
			failsafeW = ptr(v)
		}
		if d, ferr := m.egLPC.FailsafeDurationMinimum(entity); ferr == nil {
			failsafeS = ptr(d.Seconds())
		}
	} else if kind == KindLPP && m.egLPP != nil {
		l, err = m.egLPP.ProductionLimit(entity)
	} else {
		return
	}
	if err != nil && nominal == nil && failsafeW == nil && failsafeS == nil {
		return
	}
	m.updateControl(ski, func(c *store.DeviceControl) {
		if nominal != nil {
			c.LPCNominalMaxW = nominal
		}
		if failsafeW != nil {
			c.LPCFailsafeW = failsafeW
		}
		if failsafeS != nil {
			c.LPCFailsafeDurationS = failsafeS
		}
		if err != nil {
			return
		}
		if kind == KindLPC {
			c.LPCReportedW, c.LPCReportedActive = ptr(l.Value), l.IsActive
		} else {
			c.LPPReportedW, c.LPPReportedActive = ptr(math.Abs(l.Value)), l.IsActive
		}
	})
	m.mirrorLeading()
}

// updateControl mutates the Control block of a device.
func (m *Manager) updateControl(ski string, mutate func(*store.DeviceControl)) {
	m.store.Update(func(s *store.State) {
		s.UpdateDevice(ski, func(d *store.DeviceData) {
			if d.Control == nil {
				d.Control = &store.DeviceControl{}
			}
			mutate(d.Control)
		})
	})
}

// --- per-device control (API) ---

// SetDeviceLimit stages and/or applies a limit for one device. Nil arguments
// keep the current value. Setting active (true or false) always writes to the
// device; changing only the target re-applies it if the limit is active.
func (m *Manager) SetDeviceLimit(ski string, kind LimitKind, targetW *float64, active *bool, durationS *float64) error {
	ski = normaliseSKI(ski)
	if !validSKI(ski) {
		return fmt.Errorf("invalid SKI %q", ski)
	}
	if (kind == KindLPC && m.egLPC == nil) || (kind == KindLPP && m.egLPP == nil) {
		return errEGDisabled
	}
	if targetW != nil && *targetW < 0 {
		return errors.New("limit must be a positive number of watts")
	}
	wasActive := false
	m.updateControl(ski, func(c *store.DeviceControl) {
		if kind == KindLPC {
			wasActive = c.LPCActive
			if targetW != nil {
				c.LPCTargetW = *targetW
			}
			if active != nil {
				c.LPCActive = *active
			}
		} else {
			wasActive = c.LPPActive
			if targetW != nil {
				c.LPPTargetW = math.Abs(*targetW)
			}
			if active != nil {
				c.LPPActive = *active
			}
		}
		if durationS != nil {
			c.DurationS = *durationS
		}
	})
	defer m.mirrorLeading()
	if active == nil && !wasActive {
		return nil // staged only
	}
	return m.applyLimit(kind, ski, active != nil)
}

// SetDeviceConsumptionLimit is SetDeviceLimit for LPC (HTTP API adapter).
func (m *Manager) SetDeviceConsumptionLimit(ski string, targetW *float64, active *bool, durationS *float64) error {
	return m.SetDeviceLimit(ski, KindLPC, targetW, active, durationS)
}

// SetDeviceProductionLimit is SetDeviceLimit for LPP (HTTP API adapter).
func (m *Manager) SetDeviceProductionLimit(ski string, targetW *float64, active *bool, durationS *float64) error {
	return m.SetDeviceLimit(ski, KindLPP, targetW, active, durationS)
}

// effectiveLimit combines the user's limit with a §14a share: the lower one
// wins, and a limit is active if either is.
func effectiveLimit(userActive bool, userW float64, shareW *float64) (float64, bool) {
	switch {
	case userActive && shareW != nil:
		return math.Min(userW, *shareW), true
	case userActive:
		return userW, true
	case shareW != nil:
		return *shareW, true
	default:
		return 0, false
	}
}

// applyLimit writes the effective limit of a device. Without force the write
// is skipped when nothing changed, and a device we never limited is not sent a
// "deactivate" (it may be controlled by someone else).
func (m *Manager) applyLimit(kind LimitKind, ski string, force bool) error {
	m.mu.Lock()
	entity := m.egEntities[kind][ski]
	m.mu.Unlock()

	ctl := store.DeviceControl{}
	if d := m.store.Snapshot().Devices[ski]; d.Control != nil {
		ctl = *d.Control
	}
	var (
		value   float64
		active  bool
		applied *float64
	)
	durationS := ctl.DurationS
	if kind == KindLPC {
		value, active = effectiveLimit(ctl.LPCActive, ctl.LPCTargetW, ctl.DistributedLimitW)
		applied = ctl.LPCAppliedW
		if ctl.DistributedLimitW != nil {
			durationS = 0 // a §14a share lasts until the incoming limit ends
		}
	} else {
		value, active = effectiveLimit(ctl.LPPActive, ctl.LPPTargetW, nil)
		applied = ctl.LPPAppliedW
	}

	if !force {
		if !active && applied == nil {
			return nil
		}
		if active && applied != nil && *applied == value {
			return nil
		}
	}
	if entity == nil {
		// The state is kept and re-applied when the device (re)connects.
		if active && force {
			return errNotConnected
		}
		return nil
	}

	if !active && ctl.LPCNominalMaxW != nil && kind == KindLPC {
		// the value of a deactivated limit is ignored; some devices reject 0
		value = *ctl.LPCNominalMaxW
	}
	sendActive := active
	if kind == KindLPC && !active && m.managedHeatPump(ski) {
		// a heat pump the bridge manages gets "no limit" as an active limit at
		// its nominal maximum, see unlimitedLimits
		if first, _ := m.unlimitedLimits(entity, ctl); first.IsActive {
			value, sendActive = first.Value, true
		}
	}
	limit := ucapi.LoadLimit{
		Value:        value,
		IsActive:     sendActive,
		IsChangeable: true,
		Duration:     secondsToDuration(durationS),
	}
	var err error
	if kind == KindLPC {
		_, err = m.egLPC.WriteConsumptionLimit(entity, limit, m.writeResultCB(ski, kind))
	} else {
		// LPP limits are sent as negative active power (spec).
		limit.Value = -math.Abs(value)
		_, err = m.egLPP.WriteProductionLimit(entity, limit, m.writeResultCB(ski, kind))
	}
	if err != nil {
		return err
	}
	m.logger.Infof("%s limit for %s: active=%v value=%.0f W", kind, ski, active, value)
	m.updateControl(ski, func(c *store.DeviceControl) {
		var v *float64
		if active {
			v = ptr(value)
		}
		if kind == KindLPC {
			c.LPCAppliedW = v
		} else {
			c.LPPAppliedW = v
		}
	})
	return nil
}

func (m *Manager) writeResultCB(ski string, kind LimitKind) func(model.ResultDataType, model.MsgCounterType) {
	return func(r model.ResultDataType, _ model.MsgCounterType) {
		if r.ErrorNumber != nil && *r.ErrorNumber != model.ErrorNumberTypeNoError {
			desc := ""
			if r.Description != nil {
				desc = string(*r.Description)
			}
			m.logger.Errorf("%s rejected %s limit: code=%v %s", ski, kind, *r.ErrorNumber, desc)
		}
	}
}

// SetDeviceFailsafe writes the consumption failsafe power and/or minimum
// duration to a device. A device that received a limit from the bridge falls
// back to these values when the bridge's heartbeat stops (e.g. while the
// add-on restarts), so for a wallbox the failsafe power decides how fast it
// may charge during such an outage.
func (m *Manager) SetDeviceFailsafe(ski string, powerW, durationS *float64) error {
	ski = normaliseSKI(ski)
	if !validSKI(ski) {
		return fmt.Errorf("invalid SKI %q", ski)
	}
	if m.egLPC == nil {
		return errEGDisabled
	}
	m.mu.Lock()
	entity := m.egEntities[KindLPC][ski]
	m.mu.Unlock()
	if entity == nil {
		return errors.New("device is not connected or does not accept consumption limits")
	}
	if powerW != nil {
		if *powerW < 0 {
			return errors.New("failsafe power must be >= 0 W")
		}
		if _, err := m.egLPC.WriteFailsafeConsumptionActivePowerLimit(entity, *powerW); err != nil {
			return err
		}
	}
	if durationS != nil {
		if *durationS < 0 {
			return errors.New("failsafe duration must be >= 0 s")
		}
		if _, err := m.egLPC.WriteFailsafeDurationMinimum(entity, secondsToDuration(*durationS)); err != nil {
			return err
		}
	}
	m.updateControl(ski, func(c *store.DeviceControl) {
		if powerW != nil {
			c.LPCFailsafeW = ptr(*powerW)
		}
		if durationS != nil {
			c.LPCFailsafeDurationS = ptr(*durationS)
		}
	})
	return nil
}

// --- leading inverter (legacy inverter_control endpoints/entities) ---

// leadingEGSKI resolves the device that the bridge-level inverter_control acts
// on: inverter_ski if configured (even while offline, so targets can be
// staged), else the leading inverter, else the first inverter that accepts
// limits. Wallboxes and heat pumps are never chosen here.
func (m *Manager) leadingEGSKI(kind LimitKind) string {
	if m.cfg.InverterSKI != "" {
		return m.cfg.InverterSKI
	}
	m.mu.Lock()
	primary := m.primarySKI
	candidates := make([]string, 0, len(m.egEntities[kind]))
	for ski := range m.egEntities[kind] {
		candidates = append(candidates, ski)
	}
	m.mu.Unlock()
	if slices.Contains(candidates, primary) {
		return primary
	}
	slices.Sort(candidates)
	for _, ski := range candidates {
		if m.categoryOf(ski) == store.CategoryInverter {
			return ski
		}
	}
	return ""
}

// seedLeadingTargets copies targets staged on the bridge-level controls while
// no inverter was known onto the leading inverter once it appears.
func (m *Manager) seedLeadingTargets() {
	ic := m.store.Snapshot().InverterControl
	for _, kind := range []LimitKind{KindLPC, KindLPP} {
		ski := m.leadingEGSKI(kind)
		if ski == "" {
			continue
		}
		m.updateControl(ski, func(c *store.DeviceControl) {
			if kind == KindLPC && c.LPCTargetW == 0 && ic.ConsumptionTargetW != 0 {
				c.LPCTargetW = ic.ConsumptionTargetW
			}
			if kind == KindLPP && c.LPPTargetW == 0 && ic.ProductionTargetW != 0 {
				c.LPPTargetW = ic.ProductionTargetW
			}
			if c.DurationS == 0 {
				c.DurationS = ic.DurationS
			}
		})
	}
}

// mirrorLeading reflects the leading inverter's control into the legacy
// top-level inverter_control block.
func (m *Manager) mirrorLeading() {
	lpcSKI, lppSKI := m.leadingEGSKI(KindLPC), m.leadingEGSKI(KindLPP)
	snap := m.store.Snapshot()
	m.store.Update(func(s *store.State) {
		ic := s.InverterControl
		ic.Supported = m.egLPC != nil || m.egLPP != nil
		if d := snap.Devices[lpcSKI]; lpcSKI != "" && d.Control != nil {
			c := d.Control
			ic.ConsumptionTargetW = c.LPCTargetW
			ic.ConsumptionActive = c.LPCActive
			ic.ConsumptionLimitW = deref(c.LPCAppliedW, deref(c.LPCReportedW, 0))
			ic.DurationS = c.DurationS
		}
		if d := snap.Devices[lppSKI]; lppSKI != "" && d.Control != nil {
			c := d.Control
			ic.ProductionTargetW = c.LPPTargetW
			ic.ProductionActive = c.LPPActive
			ic.ProductionLimitW = deref(c.LPPAppliedW, deref(c.LPPReportedW, 0))
			ic.DurationS = c.DurationS
		}
		s.InverterControl = ic
	})
}

func deref(p *float64, fallback float64) float64 {
	if p == nil {
		return fallback
	}
	return *p
}

// SetInverterConsumptionLimit writes an LPC limit to the leading inverter.
func (m *Manager) SetInverterConsumptionLimit(valueW float64, active bool, durationS float64) error {
	if m.egLPC == nil {
		return errEGDisabled
	}
	ski := m.leadingEGSKI(KindLPC)
	if ski == "" {
		return errNoInverter
	}
	return m.SetDeviceLimit(ski, KindLPC, &valueW, &active, &durationS)
}

// SetInverterProductionLimit writes an LPP limit (positive watts) to the
// leading inverter.
func (m *Manager) SetInverterProductionLimit(valueW float64, active bool, durationS float64) error {
	if m.egLPP == nil {
		return errEGDisabled
	}
	ski := m.leadingEGSKI(KindLPP)
	if ski == "" {
		return errNoInverter
	}
	v := math.Abs(valueW)
	return m.SetDeviceLimit(ski, KindLPP, &v, &active, &durationS)
}

// SetEGConsumptionTarget stages the leading inverter's consumption limit. It
// works without an inverter (staged on the bridge until one appears).
func (m *Manager) SetEGConsumptionTarget(valueW float64, durationS *float64) error {
	return m.stageLeading(KindLPC, valueW, durationS)
}

// SetEGProductionTarget stages the leading inverter's production limit.
func (m *Manager) SetEGProductionTarget(valueW float64, durationS *float64) error {
	return m.stageLeading(KindLPP, valueW, durationS)
}

func (m *Manager) stageLeading(kind LimitKind, valueW float64, durationS *float64) error {
	if (kind == KindLPC && m.egLPC == nil) || (kind == KindLPP && m.egLPP == nil) {
		return errEGDisabled
	}
	if ski := m.leadingEGSKI(kind); ski != "" {
		return m.SetDeviceLimit(ski, kind, &valueW, nil, durationS)
	}
	m.store.Update(func(s *store.State) {
		if kind == KindLPC {
			s.InverterControl.ConsumptionTargetW = valueW
		} else {
			s.InverterControl.ProductionTargetW = valueW
		}
		if durationS != nil {
			s.InverterControl.DurationS = *durationS
		}
	})
	return nil
}

// ActivateEGConsumption applies (or removes) the leading inverter's staged
// consumption limit.
func (m *Manager) ActivateEGConsumption(active bool) error {
	return m.activateLeading(KindLPC, active)
}

// ActivateEGProduction applies (or removes) the leading inverter's staged
// production limit.
func (m *Manager) ActivateEGProduction(active bool) error {
	return m.activateLeading(KindLPP, active)
}

func (m *Manager) activateLeading(kind LimitKind, active bool) error {
	if (kind == KindLPC && m.egLPC == nil) || (kind == KindLPP && m.egLPP == nil) {
		return errEGDisabled
	}
	ski := m.leadingEGSKI(kind)
	if ski == "" {
		return errNoInverter
	}
	return m.SetDeviceLimit(ski, kind, nil, &active, nil)
}
