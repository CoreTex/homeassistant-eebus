package eebus

import (
	"testing"
	"time"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/config"

	ucapi "github.com/enbility/eebus-go/usecases/api"
	ucmocks "github.com/enbility/eebus-go/usecases/mocks"
	spineapi "github.com/enbility/spine-go/api"
	spinemocks "github.com/enbility/spine-go/mocks"
	"github.com/enbility/spine-go/model"
	"github.com/stretchr/testify/mock"
)

// A heat pump that rejects the deactivated limit gets an active one at its
// nominal maximum (Bosch CS5800i, #7).
func TestTakeoverFallsBackToActiveNominalLimit(t *testing.T) {
	m := newTestManager(t, config.Config{})
	lpc := ucmocks.NewEgLPCInterface(t)
	m.egLPC = lpc
	ski := "0123456789abcdef0123456789abcdef01234567"

	lpc.EXPECT().ConsumptionNominalMax(mock.Anything).Return(16200, nil)
	written := make(chan ucapi.LoadLimit, 1)
	lpc.EXPECT().WriteConsumptionLimit(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ spineapi.EntityRemoteInterface, l ucapi.LoadLimit, _ func(model.ResultDataType, model.MsgCounterType)) (*model.MsgCounterType, error) {
			written <- l
			return nil, nil
		}).Once()

	cb := m.takeoverResultCB(ski, spinemocks.NewEntityRemoteInterface(t))
	cb(model.ResultDataType{ErrorNumber: ptr(model.ErrorNumberTypeNoError)}, 0) // accepted: nothing
	select {
	case <-written:
		t.Fatal("an accepted limit must not be re-sent")
	case <-time.After(50 * time.Millisecond):
	}

	cb(model.ResultDataType{ErrorNumber: ptr(model.ErrorNumberType(7))}, 0)
	select {
	case l := <-written:
		if !l.IsActive || l.Value != 16200 {
			t.Fatalf("fallback limit: %+v", l)
		}
	case <-time.After(time.Second):
		t.Fatal("no fallback limit written")
	}
}
