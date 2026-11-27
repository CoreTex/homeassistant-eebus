package eebus

import (
	"encoding/json"
	"strings"
	"testing"

	shipmocks "github.com/enbility/ship-go/mocks"
	"github.com/enbility/spine-go/model"
	"github.com/enbility/spine-go/spine"
	"github.com/enbility/spine-go/util"
	"github.com/stretchr/testify/mock"
)

func TestSpineDumpShowsReportedData(t *testing.T) {
	m := newRealManager(t, nil)
	if got := m.spineDevices(); len(got) != 0 {
		t.Fatalf("no remote devices expected, got %+v", got)
	}

	// a remote battery entity with a measurement server, like the KOSTAL [2,1,1]
	local := m.svc.LocalDevice()
	writer := shipmocks.NewShipConnectionDataWriterInterface(t)
	writer.EXPECT().WriteShipMessageWithPayload(mock.Anything).Return().Maybe()
	remote := spine.NewDeviceRemote(local, "abcabcabcabcabcabcabcabcabcabcabcabcabca", spine.NewSender(writer))
	devAddr := model.AddressDeviceType("d:_i:test_battery")
	detailed := &model.NodeManagementDetailedDiscoveryDataType{
		DeviceInformation: &model.NodeManagementDetailedDiscoveryDeviceInformationType{
			Description: &model.NetworkManagementDeviceDescriptionDataType{
				DeviceAddress: &model.DeviceAddressType{Device: &devAddr},
				DeviceType:    util.Ptr(model.DeviceTypeTypeGeneric),
			},
		},
		EntityInformation: []model.NodeManagementDetailedDiscoveryEntityInformationType{{
			Description: &model.NetworkManagementEntityDescriptionDataType{
				EntityAddress: &model.EntityAddressType{Device: &devAddr, Entity: []model.AddressEntityType{2, 1, 1}},
				EntityType:    util.Ptr(model.EntityTypeTypeBattery),
			},
		}},
		FeatureInformation: []model.NodeManagementDetailedDiscoveryFeatureInformationType{{
			Description: &model.NetworkManagementFeatureDescriptionDataType{
				FeatureAddress: &model.FeatureAddressType{
					Device: &devAddr, Entity: []model.AddressEntityType{2, 1, 1}, Feature: util.Ptr(model.AddressFeatureType(11)),
				},
				FeatureType: util.Ptr(model.FeatureTypeTypeMeasurement),
				Role:        util.Ptr(model.RoleTypeServer),
				SupportedFunction: []model.FunctionPropertyType{{
					Function:           util.Ptr(model.FunctionTypeMeasurementDescriptionListData),
					PossibleOperations: &model.PossibleOperationsType{Read: &model.PossibleOperationsReadType{}},
				}},
			},
		}},
	}
	entities, err := remote.AddEntityAndFeatures(true, detailed, nil)
	if err != nil || len(entities) != 1 {
		t.Fatalf("add entity: %v %d", err, len(entities))
	}
	remote.UpdateDevice(detailed.DeviceInformation.Description)
	local.AddRemoteDeviceForSki(remote.Ski(), remote)

	feature := remote.FeatureByEntityTypeAndRole(entities[0], model.FeatureTypeTypeMeasurement, model.RoleTypeServer)
	if _, err := feature.UpdateData(true, model.FunctionTypeMeasurementDescriptionListData, &model.MeasurementDescriptionListDataType{
		MeasurementDescriptionData: []model.MeasurementDescriptionDataType{{
			MeasurementId: util.Ptr(model.MeasurementIdType(3)),
			ScopeType:     util.Ptr(model.ScopeTypeTypeStateOfCharge),
		}},
	}, nil, nil); err != nil {
		t.Fatal(err)
	}

	devices := m.spineDevices()
	if len(devices) != 1 || devices[0].DeviceType != "Generic" {
		t.Fatalf("unexpected dump: %+v", devices)
	}
	ent := devices[0].Entities[len(devices[0].Entities)-1]
	if ent.Type != "Battery" || len(ent.Address) != 3 || ent.Address[2] != 1 {
		t.Fatalf("battery entity not dumped: %+v", ent)
	}
	raw, err := json.Marshal(m.SpineDump())
	if err != nil {
		t.Fatalf("dump must serialise: %v", err)
	}
	if !strings.Contains(string(raw), `"measurementDescriptionListData"`) || !strings.Contains(string(raw), `"stateOfCharge"`) {
		t.Errorf("reported measurement description missing in %s", raw)
	}
}

func TestIsNil(t *testing.T) {
	var p *model.MeasurementListDataType
	if !isNil(nil) || !isNil(p) || isNil(1) || isNil(&model.MeasurementListDataType{}) {
		t.Error("isNil wrong")
	}
}
