package eebus

import (
	"reflect"
	"slices"

	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
)

// SpineFeature is the data a remote server feature has sent to the bridge.
type SpineFeature struct {
	Feature uint           `json:"feature"`
	Type    string         `json:"type"`
	Role    string         `json:"role"`
	Data    map[string]any `json:"data,omitempty"`
}

// SpineEntity is one entity of a remote device.
type SpineEntity struct {
	Address  []uint         `json:"address"`
	Type     string         `json:"type"`
	Features []SpineFeature `json:"features"`
}

// SpineDevice is the SPINE view of a remote device, as cached by the bridge.
type SpineDevice struct {
	SKI        string        `json:"ski"`
	Address    string        `json:"address,omitempty"`
	DeviceType string        `json:"device_type,omitempty"`
	Entities   []SpineEntity `json:"entities"`
}

// SpineDump returns everything the bridge received from its connected remote
// devices: entities, features and the latest data per function (measurement
// descriptions and values, electrical connection, load control, ...). It is
// meant for diagnostics - it shows what a device really reports without a
// trace log.
func (m *Manager) SpineDump() any { return m.spineDevices() }

func (m *Manager) spineDevices() []SpineDevice {
	var out []SpineDevice
	for _, dev := range m.svc.LocalDevice().RemoteDevices() {
		if dev == nil {
			continue
		}
		out = append(out, dumpDevice(dev))
	}
	slices.SortFunc(out, func(a, b SpineDevice) int {
		switch {
		case a.SKI < b.SKI:
			return -1
		case a.SKI > b.SKI:
			return 1
		}
		return 0
	})
	return out
}

func dumpDevice(dev spineapi.DeviceRemoteInterface) SpineDevice {
	d := SpineDevice{SKI: dev.Ski(), Entities: []SpineEntity{}}
	if addr := dev.Address(); addr != nil {
		d.Address = string(*addr)
	}
	if dt := dev.DeviceType(); dt != nil {
		d.DeviceType = string(*dt)
	}
	for _, e := range dev.Entities() {
		if e == nil {
			continue
		}
		ent := SpineEntity{Type: string(e.EntityType()), Features: []SpineFeature{}}
		if a := e.Address(); a != nil {
			for _, x := range a.Entity {
				ent.Address = append(ent.Address, uint(x))
			}
		}
		for _, f := range e.Features() {
			if f == nil {
				continue
			}
			feat := SpineFeature{Type: string(f.Type()), Role: string(f.Role())}
			if a := f.Address(); a != nil && a.Feature != nil {
				feat.Feature = uint(*a.Feature)
			}
			// only server features hold data the device reported
			if f.Role() == model.RoleTypeServer {
				for fn := range f.Operations() {
					if data := f.DataCopy(fn); !isNil(data) {
						if feat.Data == nil {
							feat.Data = map[string]any{}
						}
						feat.Data[string(fn)] = data
					}
				}
			}
			ent.Features = append(ent.Features, feat)
		}
		d.Entities = append(d.Entities, ent)
	}
	return d
}

func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		return rv.IsNil()
	}
	return false
}
