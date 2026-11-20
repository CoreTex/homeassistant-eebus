package eebus

import (
	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"

	eebusapi "github.com/enbility/eebus-go/api"
	mgcpuc "github.com/enbility/eebus-go/usecases/ma/mgcp"
	spineapi "github.com/enbility/spine-go/api"
)

// --- MGCP: Monitoring of the Grid Connection Point (smart meter / gateway,
// or an inverter with an attached energy meter). Power is positive when the
// home imports from the grid and negative when it feeds in. ---

func (m *Manager) onMGCP(_ string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	if m.mgcp == nil || device == nil {
		return
	}
	m.noteDevice(device, ucMGCP)
	m.store.Update(func(s *store.State) {
		s.UpdateDevice(device.Ski(), func(d *store.DeviceData) {
			if d.Grid == nil {
				d.Grid = &store.GridState{}
			}
			g := d.Grid
			g.Supported = true
			switch event {
			case mgcpuc.DataUpdatePower:
				if v, err := m.mgcp.Power(entity); err == nil {
					g.PowerW = ptr(v)
				}
			case mgcpuc.DataUpdatePowerLimitationFactor:
				if v, err := m.mgcp.PowerLimitationFactor(entity); err == nil {
					g.PowerLimitationFactor = ptr(v)
				}
			case mgcpuc.DataUpdateEnergyFeedIn:
				if v, err := m.mgcp.EnergyFeedIn(entity); err == nil {
					g.EnergyFeedInWh = ptr(v)
				}
			case mgcpuc.DataUpdateEnergyConsumed:
				if v, err := m.mgcp.EnergyConsumed(entity); err == nil {
					g.EnergyConsumedWh = ptr(v)
				}
			case mgcpuc.DataUpdateCurrentPerPhase:
				if v, err := m.mgcp.CurrentPerPhase(entity); err == nil {
					g.CurrentPerPhaseA = v
				}
			case mgcpuc.DataUpdateVoltagePerPhase:
				if v, err := m.mgcp.VoltagePerPhase(entity); err == nil {
					g.VoltagePerPhaseV = v
				}
			case mgcpuc.DataUpdateFrequency:
				if v, err := m.mgcp.Frequency(entity); err == nil {
					g.FrequencyHz = ptr(v)
				}
			}
		})
	})
}
