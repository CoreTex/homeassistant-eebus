package eebus

import (
	"time"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"

	eebusapi "github.com/enbility/eebus-go/api"
	ucapi "github.com/enbility/eebus-go/usecases/api"
	evccuc "github.com/enbility/eebus-go/usecases/cem/evcc"
	evcemuc "github.com/enbility/eebus-go/usecases/cem/evcem"
	evseccuc "github.com/enbility/eebus-go/usecases/cem/evsecc"
	evsocuc "github.com/enbility/eebus-go/usecases/cem/evsoc"
	opevuc "github.com/enbility/eebus-go/usecases/cem/opev"
	vabduc "github.com/enbility/eebus-go/usecases/cem/vabd"
	vapduc "github.com/enbility/eebus-go/usecases/cem/vapd"
	cslpc "github.com/enbility/eebus-go/usecases/cs/lpc"
	cslpp "github.com/enbility/eebus-go/usecases/cs/lpp"
	eglpc "github.com/enbility/eebus-go/usecases/eg/lpc"
	eglpp "github.com/enbility/eebus-go/usecases/eg/lpp"
	mgcpuc "github.com/enbility/eebus-go/usecases/ma/mgcp"
	mampc "github.com/enbility/eebus-go/usecases/ma/mpc"
	spineapi "github.com/enbility/spine-go/api"
)

// setupUseCases instantiates and registers every enabled use case on the local
// CEM entity. Per the eebus-go cs/lpc documentation, monitoring use cases (MPC,
// VABD, VAPD) are registered before the controllable-system use cases.
func (m *Manager) setupUseCases() {
	le := m.localEntity

	if m.cfg.EnableMPC {
		m.mpc = mampc.NewMPC(le, m.onMPC)
		m.svc.AddUseCase(m.mpc)
		m.store.Update(func(s *store.State) { s.MPC.Supported = true })
	}
	if m.cfg.EnableBattery {
		m.vabd = vabduc.NewVABD(le, m.onVABD)
		m.svc.AddUseCase(m.vabd)
		m.store.Update(func(s *store.State) { s.Battery.Supported = true })
	}
	if m.cfg.EnableInverter {
		m.vapd = vapduc.NewVAPD(le, m.onVAPD)
		m.svc.AddUseCase(m.vapd)
		m.store.Update(func(s *store.State) { s.Inverter.Supported = true })
	}

	if m.cfg.EnableMGCP {
		m.mgcp = mgcpuc.NewMGCP(le, m.onMGCP)
		m.svc.AddUseCase(m.mgcp)
	}
	if m.cfg.EnableEV {
		// Wallbox + vehicle: state, measurements, SoC and charging-current
		// control (OPEV). OSCEV/CEVC are intentionally not announced: without
		// ISO 15118-20 vehicles they add nothing and EVSEs may change their
		// behaviour when a CEM claims to support them.
		m.evsecc = evseccuc.NewEVSECC(le, m.onEVSECC)
		m.svc.AddUseCase(m.evsecc)
		m.evcc = evccuc.NewEVCC(m.svc, le, m.onEVCC)
		m.svc.AddUseCase(m.evcc)
		m.evcem = evcemuc.NewEVCEM(m.svc, le, m.onEVCEM)
		m.svc.AddUseCase(m.evcem)
		m.evsoc = evsocuc.NewEVSOC(le, m.onEVSOC)
		m.svc.AddUseCase(m.evsoc)
		m.opev = opevuc.NewOPEV(le, m.onOPEV)
		m.svc.AddUseCase(m.opev)
	}

	if m.cfg.EnableLPC {
		m.csLPC = cslpc.NewLPC(le, m.onCsLPC)
		m.svc.AddUseCase(m.csLPC)
		// seed spec-valid failsafe defaults; users override via the API/entities.
		_ = m.csLPC.SetConsumptionLimit(ucapi.LoadLimit{IsChangeable: true, IsActive: false})
		_ = m.csLPC.SetFailsafeConsumptionActivePowerLimit(4200, true)
		_ = m.csLPC.SetFailsafeDurationMinimum(2*time.Hour, true)
		if m.cfg.LpcNominalMaxW > 0 {
			_ = m.csLPC.SetConsumptionNominalMax(m.cfg.LpcNominalMaxW)
		}
		m.refreshLPC()
	}
	if m.cfg.EnableLPP {
		m.csLPP = cslpp.NewLPP(le, m.onCsLPP)
		m.svc.AddUseCase(m.csLPP)
		_ = m.csLPP.SetProductionLimit(ucapi.LoadLimit{IsChangeable: true, IsActive: false})
		_ = m.csLPP.SetFailsafeProductionActivePowerLimit(4200, true)
		_ = m.csLPP.SetFailsafeDurationMinimum(2*time.Hour, true)
		if m.cfg.LppNominalMaxW > 0 {
			_ = m.csLPP.SetProductionNominalMax(m.cfg.LppNominalMaxW)
		}
		m.refreshLPP()
	}

	if m.cfg.EnableEGControl {
		m.egLPC = eglpc.NewLPC(le, m.onEgLPC)
		m.svc.AddUseCase(m.egLPC)
		m.egLPP = eglpp.NewLPP(le, m.onEgLPP)
		m.svc.AddUseCase(m.egLPP)
		m.store.Update(func(s *store.State) { s.InverterControl.Supported = true })
	}
}

// --- Controllable System: LPC (receive consumption limits) ---

func (m *Manager) onCsLPC(_ string, device spineapi.DeviceRemoteInterface, _ spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	// Support/limit events come from the remote Energy Guard (control box,
	// grid operator gateway) that limits us. Heartbeat events are not used for
	// this: cs-lpc watches the heartbeat of every device with a diagnosis
	// server (inverters and wallboxes too).
	if event == cslpc.UseCaseSupportUpdate || event == cslpc.DataUpdateLimit {
		m.noteDevice(device, ucLPCSource)
	}
	switch event {
	case cslpc.LimitWriteApprovalRequired:
		if m.cfg.AutoApproveLimits {
			for mc := range m.csLPC.PendingConsumptionLimits() {
				m.csLPC.ApproveOrDenyConsumptionLimit(mc, true, "")
			}
		}
	case cslpc.ConfigurationWriteApprovalRequired:
		if m.cfg.AutoApproveLimits {
			for mc := range m.csLPC.PendingDeviceConfigurations() {
				m.csLPC.ApproveOrDenyDeviceConfiguration(mc, true, "")
			}
		}
	case cslpc.DataUpdateLimit,
		cslpc.DataUpdateFailsafeConsumptionActivePowerLimit,
		cslpc.DataUpdateFailsafeDurationMinimum,
		cslpc.DataUpdateHeartbeat:
		m.refreshLPC()
	}
}

func (m *Manager) refreshLPC() {
	if m.csLPC == nil {
		return
	}
	st := store.LPCState{Supported: true}
	if limit, err := m.csLPC.ConsumptionLimit(); err == nil {
		st.Limit = toLimitState(limit)
	}
	if v, _, err := m.csLPC.FailsafeConsumptionActivePowerLimit(); err == nil {
		st.FailsafePowerW = v
	}
	if d, _, err := m.csLPC.FailsafeDurationMinimum(); err == nil {
		st.FailsafeDurationS = d.Seconds()
	}
	if v, err := m.csLPC.ConsumptionNominalMax(); err == nil {
		st.NominalMaxW = v
	}
	st.HeartbeatOK = m.csLPC.IsHeartbeatWithinDuration()
	m.store.Update(func(s *store.State) { s.LPC = st })
	m.scheduleDistribution()
}

// --- Controllable System: LPP (receive production limits) ---

func (m *Manager) onCsLPP(_ string, device spineapi.DeviceRemoteInterface, _ spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	if event == cslpp.UseCaseSupportUpdate || event == cslpp.DataUpdateLimit {
		m.noteDevice(device, ucLPPSource)
	}
	switch event {
	case cslpp.LimitWriteApprovalRequired:
		if m.cfg.AutoApproveLimits {
			for mc := range m.csLPP.PendingProductionLimits() {
				m.csLPP.ApproveOrDenyProductionLimit(mc, true, "")
			}
		}
	case cslpp.ConfigurationWriteApprovalRequired:
		if m.cfg.AutoApproveLimits {
			for mc := range m.csLPP.PendingDeviceConfigurations() {
				m.csLPP.ApproveOrDenyDeviceConfiguration(mc, true, "")
			}
		}
	case cslpp.DataUpdateLimit,
		cslpp.DataUpdateFailsafeProductionActivePowerLimit,
		cslpp.DataUpdateFailsafeDurationMinimum,
		cslpp.DataUpdateHeartbeat:
		m.refreshLPP()
	}
}

func (m *Manager) refreshLPP() {
	if m.csLPP == nil {
		return
	}
	st := store.LPPState{Supported: true}
	if limit, err := m.csLPP.ProductionLimit(); err == nil {
		st.Limit = toLimitState(limit)
	}
	if v, _, err := m.csLPP.FailsafeProductionActivePowerLimit(); err == nil {
		st.FailsafePowerW = v
	}
	if d, _, err := m.csLPP.FailsafeDurationMinimum(); err == nil {
		st.FailsafeDurationS = d.Seconds()
	}
	if v, err := m.csLPP.ProductionNominalMax(); err == nil {
		st.NominalMaxW = v
	}
	st.HeartbeatOK = m.csLPP.IsHeartbeatWithinDuration()
	m.store.Update(func(s *store.State) { s.LPP = st })
}

// --- Monitoring of Power Consumption (MPC) ---
//
// Every remote reports into its own entry under State.Devices. Only the
// primary remote (the inverter, see isPrimary) is mirrored into the legacy
// top-level mpc/battery/inverter blocks, so a wallbox that also speaks MPC can
// no longer overwrite the inverter's values.

func (m *Manager) onMPC(_ string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	if m.mpc == nil || device == nil {
		return
	}
	m.noteDevice(device, ucMPC)
	apply := func(dst *store.MPCState) {
		dst.Supported = true
		switch event {
		case mampc.DataUpdatePower:
			if v, err := m.mpc.Power(entity); err == nil {
				dst.PowerW = ptr(v)
			}
		case mampc.DataUpdatePowerPerPhase:
			if v, err := m.mpc.PowerPerPhase(entity); err == nil {
				dst.PowerPerPhaseW = v
			}
		case mampc.DataUpdateEnergyConsumed:
			if v, err := m.mpc.EnergyConsumed(entity); err == nil {
				dst.EnergyConsumedWh = ptr(v)
			}
		case mampc.DataUpdateEnergyProduced:
			if v, err := m.mpc.EnergyProduced(entity); err == nil {
				dst.EnergyProducedWh = ptr(v)
			}
		case mampc.DataUpdateCurrentsPerPhase:
			if v, err := m.mpc.CurrentPerPhase(entity); err == nil {
				dst.CurrentPerPhaseA = v
			}
		case mampc.DataUpdateVoltagePerPhase:
			if v, err := m.mpc.VoltagePerPhase(entity); err == nil {
				dst.VoltagePerPhaseV = v
			}
		case mampc.DataUpdateFrequency:
			if v, err := m.mpc.Frequency(entity); err == nil {
				dst.FrequencyHz = ptr(v)
			}
		}
	}
	ski := device.Ski()
	primary := m.isPrimary(ski)
	m.store.Update(func(s *store.State) {
		s.UpdateDevice(ski, func(d *store.DeviceData) {
			if d.MPC == nil {
				d.MPC = &store.MPCState{}
			}
			apply(d.MPC)
		})
		if primary {
			apply(&s.MPC)
		}
	})
}

// --- Battery monitoring (VABD ~ MOB) ---

func (m *Manager) onVABD(_ string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	if m.vabd == nil || device == nil {
		return
	}
	m.noteDevice(device, ucVABD)
	apply := func(dst *store.BatteryState) {
		dst.Supported = true
		switch event {
		case vabduc.DataUpdatePower:
			if v, err := m.vabd.Power(entity); err == nil {
				dst.PowerW = ptr(v)
			}
		case vabduc.DataUpdateStateOfCharge:
			if v, err := m.vabd.StateOfCharge(entity); err == nil {
				dst.StateOfChargePct = ptr(v)
			}
		case vabduc.DataUpdateEnergyCharged:
			if v, err := m.vabd.EnergyCharged(entity); err == nil {
				dst.EnergyChargedWh = ptr(v)
			}
		case vabduc.DataUpdateEnergyDischarged:
			if v, err := m.vabd.EnergyDischarged(entity); err == nil {
				dst.EnergyDischargedWh = ptr(v)
			}
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

// --- Inverter / PV monitoring (VAPD ~ MOI) ---

func (m *Manager) onVAPD(_ string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	if m.vapd == nil || device == nil {
		return
	}
	m.noteDevice(device, ucVAPD)
	apply := func(dst *store.InverterState) {
		dst.Supported = true
		switch event {
		case vapduc.DataUpdatePower:
			if v, err := m.vapd.Power(entity); err == nil {
				dst.PowerW = ptr(v)
			}
		case vapduc.DataUpdatePowerNominalPeak:
			if v, err := m.vapd.PowerNominalPeak(entity); err == nil {
				dst.PowerNominalPeakW = ptr(v)
			}
		case vapduc.DataUpdatePVYieldTotal:
			if v, err := m.vapd.PVYieldTotal(entity); err == nil {
				dst.PVYieldTotalWh = ptr(v)
			}
		}
	}
	ski := device.Ski()
	primary := m.isPrimary(ski)
	m.store.Update(func(s *store.State) {
		s.UpdateDevice(ski, func(d *store.DeviceData) {
			if d.Inverter == nil {
				d.Inverter = &store.InverterState{}
			}
			apply(d.Inverter)
		})
		if primary {
			apply(&s.Inverter)
		}
	})
}

func toLimitState(l ucapi.LoadLimit) store.LimitState {
	ls := store.LimitState{
		Active:     l.IsActive,
		ValueW:     l.Value,
		DurationS:  l.Duration.Seconds(),
		Changeable: l.IsChangeable,
	}
	if l.IsActive && l.Duration > 0 {
		t := time.Now().Add(l.Duration)
		ls.ExpiresAt = &t
	}
	return ls
}

func ptr[T any](v T) *T { return &v }
