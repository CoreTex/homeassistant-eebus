package eebus

import (
	spineapi "github.com/enbility/spine-go/api"
)

// HandleEvent implements spineapi.EventHandlerInterface. It classifies every
// remote device right after its SPINE detailed discovery, independent of the
// use cases the bridge has enabled. Without this, a device that only offers
// use cases the bridge does not run (e.g. a §14a-only wallbox while
// enable_eg_control is off) would never be recognised.
func (m *Manager) HandleEvent(payload spineapi.EventPayload) {
	if payload.Device == nil || payload.ChangeType != spineapi.ElementChangeAdd {
		return
	}
	switch payload.EventType {
	case spineapi.EventTypeDeviceChange, spineapi.EventTypeEntityChange:
		m.noteDevice(payload.Device, "")
	}
}
