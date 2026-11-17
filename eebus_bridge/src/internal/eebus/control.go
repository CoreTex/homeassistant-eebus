package eebus

import (
	"errors"
	"time"
)

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
