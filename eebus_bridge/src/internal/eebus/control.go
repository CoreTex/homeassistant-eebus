package eebus

import (
	"errors"
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

// --- Energy Guard: discover the controllable inverter entity ---

func (m *Manager) onEgLPC(_ string, _ spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	switch event {
	case eglpc.UseCaseSupportUpdate:
		if slices.Contains(m.egLPC.AvailableScenariosForEntity(entity), uint(1)) {
			m.mu.Lock()
			m.egEntity = entity
			m.mu.Unlock()
			m.logger.Infof("inverter supports Energy-Guard control: %s", entity.Device().Ski())
		}
	case eglpc.DataUpdateLimit:
		if l, err := m.egLPC.ConsumptionLimit(entity); err == nil {
			m.store.Update(func(s *store.State) {
				s.InverterControl.ConsumptionLimitW = l.Value
				s.InverterControl.ConsumptionActive = l.IsActive
			})
		}
	}
}

func (m *Manager) onEgLPP(_ string, _ spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	switch event {
	case eglpp.UseCaseSupportUpdate:
		if slices.Contains(m.egLPP.AvailableScenariosForEntity(entity), uint(1)) {
			m.mu.Lock()
			m.egEntity = entity
			m.mu.Unlock()
		}
	case eglpp.DataUpdateLimit:
		if l, err := m.egLPP.ProductionLimit(entity); err == nil {
			m.store.Update(func(s *store.State) {
				s.InverterControl.ProductionLimitW = math.Abs(l.Value)
				s.InverterControl.ProductionActive = l.IsActive
			})
		}
	}
}

// SetInverterConsumptionLimit writes an LPC limit to the inverter (HA as Energy
// Guard). valueW is a positive consumption limit in watts.
func (m *Manager) SetInverterConsumptionLimit(valueW float64, active bool, durationS float64) error {
	m.mu.Lock()
	uc, entity := m.egLPC, m.egEntity
	m.mu.Unlock()
	if uc == nil {
		return errors.New("energy guard control is disabled")
	}
	if entity == nil {
		return errors.New("no controllable inverter connected")
	}
	limit := ucapi.LoadLimit{
		Value:        valueW,
		IsActive:     active,
		IsChangeable: true,
		Duration:     secondsToDuration(durationS),
	}
	if _, err := uc.WriteConsumptionLimit(entity, limit, m.writeResultCB("consumption")); err != nil {
		return err
	}
	m.store.Update(func(s *store.State) {
		s.InverterControl.ConsumptionLimitW = valueW
		s.InverterControl.ConsumptionActive = active
	})
	return nil
}

// SetInverterProductionLimit writes an LPP limit to the inverter. valueW is a
// positive watt magnitude; per the LPP spec the active power limit sent on the
// wire must be negative, so the sign is applied here.
func (m *Manager) SetInverterProductionLimit(valueW float64, active bool, durationS float64) error {
	m.mu.Lock()
	uc, entity := m.egLPP, m.egEntity
	m.mu.Unlock()
	if uc == nil {
		return errors.New("energy guard control is disabled")
	}
	if entity == nil {
		return errors.New("no controllable inverter connected")
	}
	limit := ucapi.LoadLimit{
		Value:        -math.Abs(valueW),
		IsActive:     active,
		IsChangeable: true,
		Duration:     secondsToDuration(durationS),
	}
	if _, err := uc.WriteProductionLimit(entity, limit, m.writeResultCB("production")); err != nil {
		return err
	}
	m.store.Update(func(s *store.State) {
		s.InverterControl.ProductionLimitW = math.Abs(valueW)
		s.InverterControl.ProductionActive = active
	})
	return nil
}

// SetEGConsumptionTarget stages the consumption limit value (and optional
// duration) without contacting the inverter. If a limit is currently active it
// is re-applied with the new value.
func (m *Manager) SetEGConsumptionTarget(valueW float64, durationS *float64) error {
	if m.egLPC == nil {
		return errors.New("energy guard control is disabled")
	}
	m.store.Update(func(s *store.State) {
		s.InverterControl.ConsumptionTargetW = valueW
		if durationS != nil {
			s.InverterControl.DurationS = *durationS
		}
	})
	ic := m.store.Snapshot().InverterControl
	if ic.ConsumptionActive {
		return m.SetInverterConsumptionLimit(valueW, true, ic.DurationS)
	}
	return nil
}

// SetEGProductionTarget stages the production limit value (positive watts).
func (m *Manager) SetEGProductionTarget(valueW float64, durationS *float64) error {
	if m.egLPP == nil {
		return errors.New("energy guard control is disabled")
	}
	m.store.Update(func(s *store.State) {
		s.InverterControl.ProductionTargetW = valueW
		if durationS != nil {
			s.InverterControl.DurationS = *durationS
		}
	})
	ic := m.store.Snapshot().InverterControl
	if ic.ProductionActive {
		return m.SetInverterProductionLimit(valueW, true, ic.DurationS)
	}
	return nil
}

// ActivateEGConsumption applies (or removes) the staged consumption target.
func (m *Manager) ActivateEGConsumption(active bool) error {
	ic := m.store.Snapshot().InverterControl
	return m.SetInverterConsumptionLimit(ic.ConsumptionTargetW, active, ic.DurationS)
}

// ActivateEGProduction applies (or removes) the staged production target.
func (m *Manager) ActivateEGProduction(active bool) error {
	ic := m.store.Snapshot().InverterControl
	return m.SetInverterProductionLimit(ic.ProductionTargetW, active, ic.DurationS)
}

func (m *Manager) writeResultCB(kind string) func(model.ResultDataType) {
	return func(r model.ResultDataType) {
		if r.ErrorNumber != nil && *r.ErrorNumber != model.ErrorNumberTypeNoError {
			desc := ""
			if r.Description != nil {
				desc = string(*r.Description)
			}
			m.logger.Errorf("inverter rejected %s limit: code=%v %s", kind, *r.ErrorNumber, desc)
		}
	}
}

// --- Controllable System configuration setters (failsafe / nominal max) ---

// SetLPCFailsafe updates the consumption failsafe limit and/or minimum duration.
func (m *Manager) SetLPCFailsafe(powerW, durationS *float64) error {
	if m.csLPC == nil {
		return errors.New("lpc is disabled")
	}
	if powerW != nil {
		if err := m.csLPC.SetFailsafeConsumptionActivePowerLimit(*powerW, true); err != nil {
			return err
		}
	}
	if durationS != nil {
		if err := m.csLPC.SetFailsafeDurationMinimum(secondsToDuration(*durationS), true); err != nil {
			return err
		}
	}
	m.refreshLPC()
	return nil
}

// SetLPCNominalMax sets the nominal maximum consumption power in watts.
func (m *Manager) SetLPCNominalMax(valueW float64) error {
	if m.csLPC == nil {
		return errors.New("lpc is disabled")
	}
	if err := m.csLPC.SetConsumptionNominalMax(valueW); err != nil {
		return err
	}
	m.refreshLPC()
	return nil
}

// SetLPPFailsafe updates the production failsafe limit and/or minimum duration.
func (m *Manager) SetLPPFailsafe(powerW, durationS *float64) error {
	if m.csLPP == nil {
		return errors.New("lpp is disabled")
	}
	if powerW != nil {
		if err := m.csLPP.SetFailsafeProductionActivePowerLimit(*powerW, true); err != nil {
			return err
		}
	}
	if durationS != nil {
		if err := m.csLPP.SetFailsafeDurationMinimum(secondsToDuration(*durationS), true); err != nil {
			return err
		}
	}
	m.refreshLPP()
	return nil
}

// SetLPPNominalMax sets the nominal maximum production power in watts.
func (m *Manager) SetLPPNominalMax(valueW float64) error {
	if m.csLPP == nil {
		return errors.New("lpp is disabled")
	}
	if err := m.csLPP.SetProductionNominalMax(valueW); err != nil {
		return err
	}
	m.refreshLPP()
	return nil
}

func secondsToDuration(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}
