package eebus

import (
	"slices"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"

	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
)

// Capability names stored in store.DeviceData.UseCases.
const (
	ucMPC       = "mpc"
	ucMGCP      = "mgcp"
	ucVAPD      = "vapd"
	ucVABD      = "vabd"
	ucMOI       = "moi"
	ucMOB       = "mob"
	ucEgLPC     = "eg_lpc" // device accepts consumption limits from us
	ucEgLPP     = "eg_lpp" // device accepts production limits from us
	ucEVSECC    = "evsecc"
	ucEVCC      = "evcc"
	ucEVCEM     = "evcem"
	ucEVSOC     = "evsoc"
	ucOPEV      = "opev"
	ucOHPCF     = "ohpcf"      // heat pump offers optional consumption (PV surplus)
	ucLPCSource = "lpc_source" // device sends consumption limits to us (control box)
	ucLPPSource = "lpp_source" // device sends production limits to us
)

var (
	wallboxEntities = []string{
		string(model.EntityTypeTypeEV), string(model.EntityTypeTypeEVSE),
		string(model.EntityTypeTypeChargingOutlet),
	}
	heatPumpEntities = []string{
		string(model.EntityTypeTypeHeatPumpAppliance), string(model.EntityTypeTypeCompressor),
		string(model.EntityTypeTypeHeatSourceUnit), string(model.EntityTypeTypeHeatSourceCircuit),
		string(model.EntityTypeTypeHeatSinkCircuit), string(model.EntityTypeTypeRefrigerantCircuit),
		string(model.EntityTypeTypeDHWCircuit), string(model.EntityTypeTypeHeatingCircuit),
	}
	inverterEntities = []string{
		string(model.EntityTypeTypeInverter), string(model.EntityTypeTypePVSystem),
		string(model.EntityTypeTypePV), string(model.EntityTypeTypePVString),
		string(model.EntityTypeTypePVESHybrid), string(model.EntityTypeTypeBatterySystem),
		string(model.EntityTypeTypeBattery), string(model.EntityTypeTypeElectricalStorage),
		string(model.EntityTypeTypeElectricityStorageSystem),
		string(model.EntityTypeTypeElectricityGenerationSystem),
	}
	gridEntities = []string{string(model.EntityTypeTypeGridConnectionPointOfPremises)}
	evUseCases   = []string{ucEVSECC, ucEVCC, ucEVCEM, ucEVSOC, ucOPEV}
)

// classify derives a device category. Order matters: a wallbox or heat pump
// may also report MPC, and an inverter may also host a grid meter (MGCP), so
// the most specific appliance wins.
func classify(deviceType string, entityTypes, useCases []string) string {
	anyOf := func(have, want []string) bool {
		for _, w := range want {
			if slices.Contains(have, w) {
				return true
			}
		}
		return false
	}
	switch {
	case deviceType == string(model.DeviceTypeTypeChargingStation),
		anyOf(entityTypes, wallboxEntities), anyOf(useCases, evUseCases):
		return store.CategoryWallbox
	case anyOf(entityTypes, heatPumpEntities):
		return store.CategoryHeatPump
	case deviceType == string(model.DeviceTypeTypeInverter),
		anyOf(entityTypes, inverterEntities), anyOf(useCases, []string{ucVAPD, ucVABD, ucMOI, ucMOB}):
		return store.CategoryInverter
	case anyOf(entityTypes, gridEntities), slices.Contains(useCases, ucMGCP):
		return store.CategoryGridMeter
	case deviceType == string(model.DeviceTypeTypeElectricitySupplySystem),
		slices.Contains(entityTypes, string(model.EntityTypeTypeGridGuard)),
		anyOf(useCases, []string{ucLPCSource, ucLPPSource}):
		return store.CategoryControlBox
	default:
		return store.CategoryGeneric
	}
}

// primaryEligible reports whether a device of the given category may act as
// the leading inverter when inverter_ski is not configured. Unknown devices
// stay eligible so a single generic inverter keeps working as before.
func primaryEligible(category string) bool {
	return category == store.CategoryInverter || category == store.CategoryGeneric || category == ""
}

// noteDevice records a capability (optional, "" for none) and refreshes the
// device type / entity types / category of the remote device. It is called
// from every use-case event, so the classification converges as discovery
// completes.
func (m *Manager) noteDevice(device spineapi.DeviceRemoteInterface, useCase string) {
	if device == nil || device.Ski() == "" {
		return
	}
	ski := device.Ski()

	deviceType := ""
	if dt := device.DeviceType(); dt != nil {
		deviceType = string(*dt)
	}
	var entityTypes []string
	for _, e := range device.Entities() {
		if e == nil {
			continue
		}
		et := string(e.EntityType())
		if et != "" && et != string(model.EntityTypeTypeDeviceInformation) && !slices.Contains(entityTypes, et) {
			entityTypes = append(entityTypes, et)
		}
	}
	slices.Sort(entityTypes)

	// Compute against the current snapshot first: this runs on every data
	// event, and an unchanged device must not trigger a store update/push.
	cur := m.store.Snapshot().Devices[ski]
	useCases := cur.UseCases
	if useCase != "" && !slices.Contains(useCases, useCase) {
		useCases = append(slices.Clone(useCases), useCase)
		slices.Sort(useCases)
	}
	category := classify(deviceType, entityTypes, useCases)
	if category == cur.Category && deviceType == cur.DeviceType &&
		slices.Equal(entityTypes, cur.EntityTypes) && slices.Equal(useCases, cur.UseCases) {
		return
	}
	m.store.Update(func(s *store.State) {
		s.UpdateDevice(ski, func(d *store.DeviceData) {
			uc := d.UseCases
			if useCase != "" && !slices.Contains(uc, useCase) {
				uc = append(slices.Clone(uc), useCase)
				slices.Sort(uc)
			}
			d.DeviceType, d.EntityTypes, d.UseCases = deviceType, entityTypes, uc
			d.Category = classify(deviceType, entityTypes, uc)
		})
	})
	m.logger.Debugf("device %s: category=%s type=%s entities=%v", ski, category, deviceType, entityTypes)

	// A device that turns out to be e.g. a wallbox must not stay the leading
	// inverter (unless pinned by inverter_ski).
	m.mu.Lock()
	drop := m.cfg.InverterSKI == "" && m.primarySKI == ski && !primaryEligible(category)
	if drop {
		m.primarySKI = ""
	}
	m.mu.Unlock()
	if drop {
		m.logger.Infof("device %s is a %s, no longer used as leading inverter", ski, category)
		m.syncConnections()
	}
	m.scheduleDistribution()
}

// categoryOf returns the current category of a device.
func (m *Manager) categoryOf(ski string) string {
	return m.store.Snapshot().Devices[ski].Category
}
