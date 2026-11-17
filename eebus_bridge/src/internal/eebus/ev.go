package eebus

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"

	eebusapi "github.com/enbility/eebus-go/api"
	ucapi "github.com/enbility/eebus-go/usecases/api"
	evccuc "github.com/enbility/eebus-go/usecases/cem/evcc"
	evcemuc "github.com/enbility/eebus-go/usecases/cem/evcem"
	evseccuc "github.com/enbility/eebus-go/usecases/cem/evsecc"
	evsocuc "github.com/enbility/eebus-go/usecases/cem/evsoc"
	opevuc "github.com/enbility/eebus-go/usecases/cem/opev"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
)

// Wallbox (EVSE) and vehicle (EV) support. A wallbox exposes an EVSE entity
// and, while a car is plugged in, an EV entity; both live on the wallbox's
// SPINE device, so everything is stored per wallbox SKI.

var allPhases = []model.ElectricalConnectionPhaseNameType{
	model.ElectricalConnectionPhaseNameTypeA,
	model.ElectricalConnectionPhaseNameTypeB,
	model.ElectricalConnectionPhaseNameTypeC,
}

func (m *Manager) updateEV(ski string, mutate func(*store.EVState)) {
	m.store.Update(func(s *store.State) {
		s.UpdateDevice(ski, func(d *store.DeviceData) {
			if d.EV == nil {
				d.EV = &store.EVState{}
			}
			mutate(d.EV)
		})
	})
}

func isEVEntity(entity spineapi.EntityRemoteInterface) bool {
	return entity != nil && entity.EntityType() == model.EntityTypeTypeEV
}

func (m *Manager) setEVEntity(ski string, entity spineapi.EntityRemoteInterface) {
	m.mu.Lock()
	m.evEntities[ski] = entity
	m.mu.Unlock()
}

// --- EVSECC: wallbox state ---

func (m *Manager) onEVSECC(_ string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	if m.evsecc == nil || device == nil {
		return
	}
	m.noteDevice(device, ucEVSECC)
	ski := device.Ski()
	switch event {
	case evseccuc.DataUpdateOperatingState, evseccuc.EvseConnected, evseccuc.UseCaseSupportUpdate:
		if state, msg, err := m.evsecc.OperatingState(entity); err == nil {
			m.updateEV(ski, func(ev *store.EVState) {
				ev.EVSEOperatingState, ev.EVSEErrorMessage = string(state), msg
			})
		}
	case evseccuc.EvseDisconnected:
		m.updateEV(ski, func(ev *store.EVState) { ev.EVSEOperatingState = "" })
	}
}

// --- EVCC: vehicle connection, charge state, limits ---

func (m *Manager) onEVCC(_ string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	if m.evcc == nil || device == nil {
		return
	}
	m.noteDevice(device, ucEVCC)
	ski := device.Ski()

	switch event {
	case evccuc.EvConnected, evccuc.UseCaseSupportUpdate:
		if !isEVEntity(entity) {
			return
		}
		m.setEVEntity(ski, entity)
		m.updateEV(ski, func(ev *store.EVState) { ev.Connected = m.evcc.EVConnected(entity) })
		m.refreshEV(ski, entity)
		m.logger.Infof("vehicle connected at %s", ski)
		m.reapplyEV(ski)
		m.scheduleDistribution()
	case evccuc.EvDisconnected:
		m.setEVEntity(ski, nil)
		m.updateEV(ski, func(ev *store.EVState) {
			ev.Connected = false
			ev.ChargeState = string(ucapi.EVChargeStateTypeUnplugged)
			ev.PowerW, ev.PowerPerPhaseW, ev.CurrentPerPhaseA = ptr(0.0), nil, nil
			ev.StateOfChargePct, ev.LimitCurrentA, ev.AppliedCurrentA = nil, nil, nil
			ev.MinCurrentA, ev.MaxCurrentA, ev.DefaultCurrentA = nil, nil, nil
			ev.Identifications, ev.SleepMode = nil, nil
		})
		m.logger.Infof("vehicle disconnected from %s", ski)
		m.scheduleDistribution()
	default:
		if isEVEntity(entity) {
			m.refreshEV(ski, entity)
		}
	}
}

// refreshEV reads every EV value the use cases currently provide. Reading all
// of them on each event keeps the handlers simple; the values are local
// (already received) data, so this does not cause network traffic.
func (m *Manager) refreshEV(ski string, entity spineapi.EntityRemoteInterface) {
	m.updateEV(ski, func(ev *store.EVState) {
		if m.evcc != nil {
			ev.Connected = m.evcc.EVConnected(entity)
			if v, err := m.evcc.ChargeState(entity); err == nil {
				ev.ChargeState = string(v)
			}
			if v, err := m.evcc.CommunicationStandard(entity); err == nil {
				ev.CommunicationStandard = string(v)
			}
			if v, err := m.evcc.AsymmetricChargingSupport(entity); err == nil {
				ev.AsymmetricCharging = ptr(v)
			}
			if v, err := m.evcc.IsInSleepMode(entity); err == nil {
				ev.SleepMode = ptr(v)
			}
			if items, err := m.evcc.Identifications(entity); err == nil {
				ids := make([]string, 0, len(items))
				for _, it := range items {
					if it.Value != "" {
						ids = append(ids, it.Value)
					}
				}
				ev.Identifications = ids
			}
		}
		if m.opev != nil {
			if mins, maxs, defs, err := m.opev.CurrentLimits(entity); err == nil {
				ev.MinCurrentA, ev.MaxCurrentA, ev.DefaultCurrentA = commonCurrentLimits(mins, maxs, defs)
			}
			if limits, err := m.opev.LoadControlLimits(entity); err == nil {
				ev.LimitCurrentA = lowestActiveLimit(limits)
			}
		}
		if m.evcem != nil {
			if v, err := m.evcem.PhasesConnected(entity); err == nil {
				ev.PhasesConnected = ptr(v)
			}
			if v, err := m.evcem.PowerPerPhase(entity); err == nil {
				ev.PowerPerPhaseW = v
				ev.PowerW = ptr(sum(v))
			}
			if v, err := m.evcem.CurrentPerPhase(entity); err == nil {
				ev.CurrentPerPhaseA = v
			}
			if v, err := m.evcem.EnergyCharged(entity); err == nil {
				ev.EnergyChargedWh = ptr(v)
			}
		}
		if m.evsoc != nil {
			if v, err := m.evsoc.StateOfCharge(entity); err == nil {
				ev.StateOfChargePct = ptr(v)
			}
		}
	})
}

// commonCurrentLimits reduces per-phase limits to values valid on every
// phase: the highest minimum, the lowest maximum and the first default.
func commonCurrentLimits(mins, maxs, defs []float64) (minA, maxA, defA *float64) {
	if len(mins) > 0 {
		minA = ptr(slices.Max(mins))
	}
	if len(maxs) > 0 {
		maxA = ptr(slices.Min(maxs))
	}
	if len(defs) > 0 {
		defA = ptr(defs[0])
	}
	return minA, maxA, defA
}

// lowestActiveLimit returns the lowest active phase limit (nil if none).
func lowestActiveLimit(limits []ucapi.LoadLimitsPhase) *float64 {
	var out *float64
	for _, l := range limits {
		if l.IsActive && (out == nil || l.Value < *out) {
			out = ptr(l.Value)
		}
	}
	return out
}

func sum(v []float64) float64 {
	t := 0.0
	for _, x := range v {
		t += x
	}
	return t
}

// --- EVCEM / EVSOC: measurements ---

func (m *Manager) onEVCEM(_ string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	if m.evcem == nil || device == nil {
		return
	}
	m.noteDevice(device, ucEVCEM)
	if event != evcemuc.UseCaseSupportUpdate && isEVEntity(entity) {
		m.refreshEV(device.Ski(), entity)
	}
}

func (m *Manager) onEVSOC(_ string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	if m.evsoc == nil || device == nil {
		return
	}
	m.noteDevice(device, ucEVSOC)
	if event == evsocuc.DataUpdateStateOfCharge && isEVEntity(entity) {
		if v, err := m.evsoc.StateOfCharge(entity); err == nil {
			m.updateEV(device.Ski(), func(ev *store.EVState) { ev.StateOfChargePct = ptr(v) })
		}
	}
}

// --- OPEV: charging current control ---

func (m *Manager) onOPEV(_ string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	if m.opev == nil || device == nil {
		return
	}
	m.noteDevice(device, ucOPEV)
	ski := device.Ski()
	switch event {
	case opevuc.UseCaseSupportUpdate:
		if !isEVEntity(entity) {
			return
		}
		m.setEVEntity(ski, entity)
		m.updateEV(ski, func(ev *store.EVState) { ev.ControlSupported = true })
		m.refreshEV(ski, entity)
		m.reapplyEV(ski)
		m.scheduleDistribution()
	default:
		if isEVEntity(entity) {
			m.refreshEV(ski, entity)
		}
	}
}

// SetEVCurrent sets the charging current Home Assistant wants for a wallbox,
// in amperes per phase. 0 pauses charging; nil hands control back to the
// wallbox (the bridge removes its limit). The value is capped by a §14a share
// and applied whenever a vehicle is connected.
func (m *Manager) SetEVCurrent(ski string, currentA *float64) error {
	ski = normaliseSKI(ski)
	if !validSKI(ski) {
		return fmt.Errorf("invalid SKI %q", ski)
	}
	if m.opev == nil {
		return errors.New("wallbox control is disabled (enable_ev)")
	}
	if currentA != nil && (*currentA < 0 || math.IsNaN(*currentA)) {
		return errors.New("current must be >= 0 A")
	}
	m.updateEV(ski, func(ev *store.EVState) { ev.RequestedCurrentA = currentA })
	return m.applyEV(ski, true)
}

// effectiveCurrent combines the requested current with a §14a cap.
func effectiveCurrent(requested, capA *float64) (*float64, bool) {
	switch {
	case requested != nil && capA != nil:
		return ptr(math.Min(*requested, *capA)), true
	case requested != nil:
		return ptr(*requested), true
	case capA != nil:
		return ptr(*capA), true
	default:
		return nil, false
	}
}

// reapplyEV re-sends our current after a (re)connect. It never sends a
// "release" on its own, so a wallbox Home Assistant does not manage is left
// alone.
func (m *Manager) reapplyEV(ski string) {
	ev := store.EVState{}
	if d := m.store.Snapshot().Devices[ski]; d.EV != nil {
		ev = *d.EV
	}
	_, active := effectiveCurrent(ev.RequestedCurrentA, ev.CapCurrentA)
	if err := m.applyEV(ski, active); err != nil {
		m.logger.Errorf("applying charging current to %s: %v", ski, err)
	}
}

// applyEV writes the effective charging current to the connected vehicle.
// Without force it only writes when the value changed.
func (m *Manager) applyEV(ski string, force bool) error {
	m.mu.Lock()
	entity := m.evEntities[ski]
	m.mu.Unlock()

	ev := store.EVState{}
	if d := m.store.Snapshot().Devices[ski]; d.EV != nil {
		ev = *d.EV
	}
	value, active := effectiveCurrent(ev.RequestedCurrentA, ev.CapCurrentA)
	if !active && ev.AppliedCurrentA == nil && !force {
		return nil
	}
	if !force && active && ev.AppliedCurrentA != nil && *ev.AppliedCurrentA == *value {
		return nil
	}
	if entity == nil || m.opev == nil {
		return nil // stored; applied once a vehicle connects
	}

	phases := allPhases
	if limits, err := m.opev.LoadControlLimits(entity); err == nil && len(limits) > 0 {
		phases = phases[:0:0]
		for _, l := range limits {
			phases = append(phases, l.Phase)
		}
	}
	writes := make([]ucapi.LoadLimitsPhase, 0, len(phases))
	for _, p := range phases {
		l := ucapi.LoadLimitsPhase{Phase: p, IsActive: active}
		if active {
			// Values below the vehicle minimum are replaced by the wallbox's
			// default (usually 0 A = pause) inside eebus-go.
			l.Value = math.Floor(*value)
		}
		writes = append(writes, l)
	}
	if _, err := m.opev.WriteLoadControlLimits(entity, writes, m.evWriteResultCB(ski)); err != nil {
		return err
	}
	if active {
		m.logger.Infof("charging current for %s: %.0f A", ski, math.Floor(*value))
	} else {
		m.logger.Infof("charging current for %s: released to wallbox", ski)
	}
	m.updateEV(ski, func(e *store.EVState) {
		if active {
			e.AppliedCurrentA = ptr(math.Floor(*value))
		} else {
			e.AppliedCurrentA = nil
		}
	})
	return nil
}

func (m *Manager) evWriteResultCB(ski string) func(model.ResultDataType, model.MsgCounterType) {
	return func(r model.ResultDataType, _ model.MsgCounterType) {
		if r.ErrorNumber != nil && *r.ErrorNumber != model.ErrorNumberTypeNoError {
			desc := ""
			if r.Description != nil {
				desc = string(*r.Description)
			}
			m.logger.Errorf("%s rejected charging current: code=%v %s", ski, *r.ErrorNumber, desc)
		}
	}
}
