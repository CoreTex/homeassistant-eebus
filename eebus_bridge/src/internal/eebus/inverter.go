package eebus

import (
	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"

	eebusapi "github.com/enbility/eebus-go/api"
	mobuc "github.com/enbility/eebus-go/usecases/ma/mob"
	moiuc "github.com/enbility/eebus-go/usecases/ma/moi"
	spineapi "github.com/enbility/spine-go/api"
)

// --- MOI: Monitoring of Inverter (AC side) ---

func (m *Manager) onMOI(_ string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	if m.moi == nil || device == nil {
		return
	}
	m.noteDevice(device, ucMOI)
	m.store.Update(func(s *store.State) {
		s.UpdateDevice(device.Ski(), func(d *store.DeviceData) {
			if d.InverterAC == nil {
				d.InverterAC = &store.InverterACState{}
			}
			ac := d.InverterAC
			ac.Supported = true
			// eebus-go reports consumption positive; the bridge shows the
			// inverter output (production) positive.
			switch event {
			case moiuc.DataUpdatePower:
				if v, err := m.moi.Power(entity); err == nil {
					ac.PowerW = ptr(-v)
				}
			case moiuc.DataUpdatePowerPerPhase:
				if v, err := m.moi.PowerPerPhase(entity); err == nil {
					ac.PowerPerPhaseW = negate(v)
				}
			case moiuc.DataUpdateEnergyProduced:
				if v, err := m.moi.EnergyProduced(entity); err == nil {
					ac.EnergyProducedWh = ptr(v)
				}
			case moiuc.DataUpdateEnergyConsumed:
				if v, err := m.moi.EnergyConsumed(entity); err == nil {
					ac.EnergyConsumedWh = ptr(v)
				}
			case moiuc.DataUpdateYieldTotal:
				if v, err := m.moi.YieldTotal(entity); err == nil {
					ac.YieldTotalWh = ptr(v)
				}
			case moiuc.DataUpdateCurrentPerPhase:
				if v, err := m.moi.CurrentPerPhase(entity); err == nil {
					ac.CurrentPerPhaseA = negate(v)
				}
			case moiuc.DataUpdateVoltagePerPhase:
				if v, err := m.moi.VoltagePerPhase(entity); err == nil {
					ac.VoltagePerPhaseV = v
				}
			case moiuc.DataUpdateFrequency:
				if v, err := m.moi.Frequency(entity); err == nil {
					ac.FrequencyHz = ptr(v)
				}
			}
		})
	})
	// outside the store update: it reads the store itself
	if event == moiuc.DataUpdatePower {
		m.redistributeOnMeasurement()
	}
}

// plausibleStateOfEnergy corrects a stored energy above the usable capacity.
// Some devices (KOSTAL PLENTICORE G3) report stateOfEnergy as SoC% x capacity
// without dividing by 100. Without a SoC the value cannot be trusted at all.
func plausibleStateOfEnergy(b *store.BatteryState) (*float64, bool) {
	if b.StateOfEnergyWh == nil || b.UsableCapacityWh == nil || *b.UsableCapacityWh <= 0 {
		return nil, false
	}
	if *b.StateOfEnergyWh <= *b.UsableCapacityWh*1.05 {
		return nil, false
	}
	if b.StateOfChargePct == nil {
		return nil, true // implausible and nothing to derive it from: hide it
	}
	v := *b.StateOfChargePct / 100 * *b.UsableCapacityWh
	return &v, true
}

func negate(v []float64) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = -x
	}
	return out
}

// --- MOB: Monitoring of Battery ---

func (m *Manager) onMOB(_ string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	if m.mob == nil || device == nil {
		return
	}
	m.noteDevice(device, ucMOB)
	apply := func(dst *store.BatteryState) {
		dst.Supported = true
		switch event {
		case mobuc.DataUpdatePower:
			if v, err := m.mob.Power(entity); err == nil {
				dst.PowerW = ptr(v) // positive = charging
			}
		case mobuc.DataUpdateEnergyCharged:
			if v, err := m.mob.EnergyCharged(entity); err == nil {
				dst.EnergyChargedWh = ptr(v)
			}
		case mobuc.DataUpdateEnergyDischarged:
			if v, err := m.mob.EnergyDischarged(entity); err == nil {
				dst.EnergyDischargedWh = ptr(v)
			}
		case mobuc.DataUpdateStateOfCharge:
			if v, err := m.mob.StateOfCharge(entity); err == nil {
				dst.StateOfChargePct = ptr(v)
			}
		case mobuc.DataUpdateStateOfHealth:
			if v, err := m.mob.StateOfHealth(entity); err == nil {
				dst.StateOfHealthPct = ptr(v)
			}
		case mobuc.DataUpdateTemperature:
			if v, err := m.mob.Temperature(entity); err == nil {
				dst.TemperatureC = ptr(v)
			}
		case mobuc.DataUpdateStateOfEnergy:
			if v, err := m.mob.StateOfEnergy(entity); err == nil {
				dst.StateOfEnergyWh = ptr(v)
			}
		case mobuc.DataUpdateUseableCapacity:
			if v, err := m.mob.UseableCapacity(entity); err == nil {
				dst.UsableCapacityWh = ptr(v)
			}
		case mobuc.DataUpdateLoadCycleCount:
			if v, err := m.mob.LoadCycleCount(entity); err == nil {
				dst.CycleCount = ptr(v)
			}
		case mobuc.DataUpdateVoltage:
			if v, err := m.mob.Voltage(entity); err == nil {
				dst.VoltageV = ptr(v)
			}
		case mobuc.DataUpdateCurrent:
			if v, err := m.mob.Current(entity); err == nil {
				dst.CurrentA = ptr(v)
			}
		}
		if fixed, ok := plausibleStateOfEnergy(dst); ok {
			if !m.soeWarned.Swap(true) {
				m.logger.Infof("battery reports a stored energy above its capacity; using state of charge x capacity instead")
			}
			dst.StateOfEnergyWh = fixed
		}
	}
	ski := device.Ski()
	primary := m.isPrimary(ski)
	m.store.Update(func(s *store.State) {
		s.UpdateDevice(ski, func(d *store.DeviceData) {
			if d.Battery == nil {
				d.Battery = &store.BatteryState{}
			}
			apply(d.Battery)
		})
		if primary {
			apply(&s.Battery)
		}
	})
}
