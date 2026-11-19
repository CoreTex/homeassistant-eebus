package eebus

import (
	"errors"
	"fmt"
	"time"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"

	eebusapi "github.com/enbility/eebus-go/api"
	ucapi "github.com/enbility/eebus-go/usecases/api"
	ohpcfuc "github.com/enbility/eebus-go/usecases/cem/ohpcf"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
)

// Heat pump flexibility (OHPCF). The compressor of a heat pump announces an
// optional consumption ("I could heat now, about X W"). The bridge only
// reports it and executes start/pause/resume/stop commands; deciding when to
// start (e.g. on PV surplus) is done in Home Assistant.

// Flex actions accepted by SetFlex.
const (
	FlexStart  = "start"
	FlexPause  = "pause"
	FlexResume = "resume"
	FlexStop   = "stop"
)

func (m *Manager) onOHPCF(_ string, device spineapi.DeviceRemoteInterface, entity spineapi.EntityRemoteInterface, event eebusapi.EventType) {
	if m.ohpcf == nil || device == nil {
		return
	}
	m.noteDevice(device, ucOHPCF)
	ski := device.Ski()
	if event == ohpcfuc.UseCaseSupportUpdate {
		m.logger.Infof("heat pump %s offers optional consumption (OHPCF)", ski)
	}
	if entity != nil && entity.EntityType() == model.EntityTypeTypeCompressor {
		m.mu.Lock()
		m.flexEntities[ski] = entity
		m.mu.Unlock()
	}
	m.refreshFlex(ski)
}

// refreshFlex reads the current offer of a heat pump into the store.
func (m *Manager) refreshFlex(ski string) {
	m.mu.Lock()
	entity := m.flexEntities[ski]
	m.mu.Unlock()
	if entity == nil || m.ohpcf == nil {
		return
	}
	st := readFlex(m.ohpcf, entity)
	m.store.Update(func(s *store.State) {
		s.UpdateDevice(ski, func(d *store.DeviceData) {
			if d.Flex != nil {
				st.LastCommand, st.LastError = d.Flex.LastCommand, d.Flex.LastError
			}
			d.Flex = &st
		})
	})
}

// flexReader is the read part of the OHPCF use case (for tests).
type flexReader interface {
	OptionalPowerConsumption(entity spineapi.EntityRemoteInterface) (*ucapi.OptionalPowerConsumptionInfo, error)
	PowerConsumptionProcessState(entity spineapi.EntityRemoteInterface) (ucapi.CompressorPowerConsumptionStateType, error)
	PowerConsumptionMinimalRunDuration(entity spineapi.EntityRemoteInterface) (time.Duration, error)
	PowerConsumptionMinimalPauseDuration(entity spineapi.EntityRemoteInterface) (time.Duration, error)
}

func readFlex(uc flexReader, entity spineapi.EntityRemoteInterface) store.FlexState {
	st := store.FlexState{Supported: true}
	info, err := uc.OptionalPowerConsumption(entity)
	if err != nil || info == nil {
		// no offer at the moment
		return st
	}
	if state, err := uc.PowerConsumptionProcessState(entity); err == nil {
		st.State = string(state)
	}
	st.Available = st.State == string(ucapi.CompressorPowerConsumptionStateAvailable) ||
		st.State == string(ucapi.CompressorPowerConsumptionStateScheduled)
	st.PowerW, st.MaxPowerW = info.Power, info.MaxPower
	st.Pausable, st.Stoppable = info.IsPausable, info.IsStoppable
	st.StartTime = info.StartTime
	if d, err := uc.PowerConsumptionMinimalRunDuration(entity); err == nil {
		st.MinRunS = ptr(d.Seconds())
	}
	if d, err := uc.PowerConsumptionMinimalPauseDuration(entity); err == nil {
		st.MinPauseS = ptr(d.Seconds())
	}
	return st
}

// SetFlex executes a command on a heat pump's optional consumption:
// "start" (optionally delayed by startIn), "pause", "resume" or "stop".
func (m *Manager) SetFlex(ski, action string, startIn time.Duration) error {
	ski = normaliseSKI(ski)
	if !validSKI(ski) {
		return fmt.Errorf("invalid SKI %q", ski)
	}
	if m.ohpcf == nil {
		return errors.New("heat pump control is disabled (enable_eg_control)")
	}
	m.mu.Lock()
	entity := m.flexEntities[ski]
	m.mu.Unlock()
	if entity == nil {
		return errors.New("this device offers no optional consumption (OHPCF)")
	}
	st := readFlex(m.ohpcf, entity)
	if st.State == "" {
		return errors.New("the heat pump currently offers no optional consumption")
	}
	if err := checkFlexAction(st, action); err != nil {
		return err
	}

	cb := m.flexResultCB(ski, action)
	var err error
	switch action {
	case FlexStart:
		_, err = m.ohpcf.SchedulePowerConsumptionProcess(entity, max(startIn, 0), cb)
	case FlexPause:
		_, err = m.ohpcf.PausePowerConsumptionProcess(entity, cb)
	case FlexResume:
		_, err = m.ohpcf.ResumePowerConsumptionProcess(entity, cb)
	case FlexStop:
		_, err = m.ohpcf.AbortPowerConsumptionProcess(entity, cb)
	}
	if err != nil {
		return err
	}
	m.logger.Infof("heat pump %s: %s optional consumption", ski, action)
	m.store.Update(func(s *store.State) {
		s.UpdateDevice(ski, func(d *store.DeviceData) {
			if d.Flex == nil {
				d.Flex = &store.FlexState{Supported: true}
			}
			d.Flex.LastCommand, d.Flex.LastError = action, ""
		})
	})
	return nil
}

// checkFlexAction rejects commands the current offer does not allow, with a
// message a user understands.
func checkFlexAction(st store.FlexState, action string) error {
	available := string(ucapi.CompressorPowerConsumptionStateAvailable)
	scheduled := string(ucapi.CompressorPowerConsumptionStateScheduled)
	running := string(ucapi.CompressorPowerConsumptionStateRunning)
	paused := string(ucapi.CompressorPowerConsumptionStatePaused)
	switch action {
	case FlexStart:
		if st.State != available && st.State != scheduled {
			return fmt.Errorf("cannot start: the optional consumption is %s", st.State)
		}
	case FlexPause:
		if !st.Pausable {
			return errors.New("the heat pump does not allow pausing")
		}
		if st.State != running {
			return fmt.Errorf("cannot pause: the optional consumption is %s", st.State)
		}
	case FlexResume:
		if st.State != paused {
			return fmt.Errorf("cannot resume: the optional consumption is %s", st.State)
		}
	case FlexStop:
		if !st.Stoppable {
			return errors.New("the heat pump does not allow stopping")
		}
		if st.State != running && st.State != paused && st.State != scheduled {
			return fmt.Errorf("cannot stop: the optional consumption is %s", st.State)
		}
	default:
		return fmt.Errorf("unknown action %q (start, pause, resume, stop)", action)
	}
	return nil
}

func (m *Manager) flexResultCB(ski, action string) func(model.ResultDataType, model.MsgCounterType) {
	return func(r model.ResultDataType, _ model.MsgCounterType) {
		if r.ErrorNumber == nil || *r.ErrorNumber == model.ErrorNumberTypeNoError {
			return
		}
		desc := ""
		if r.Description != nil {
			desc = string(*r.Description)
		}
		m.logger.Errorf("heat pump %s rejected %s: code=%v %s", ski, action, *r.ErrorNumber, desc)
		m.store.Update(func(s *store.State) {
			s.UpdateDevice(ski, func(d *store.DeviceData) {
				if d.Flex != nil {
					d.Flex.LastError = fmt.Sprintf("%s rejected (code %v) %s", action, *r.ErrorNumber, desc)
				}
			})
		})
	}
}
