package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"
)

// stubController records calls and can be told to fail.
type stubController struct {
	calls    []string
	failNext bool

	lastConsW   float64
	lastActive  bool
	lastTrusted string
}

func (s *stubController) maybeFail(name string) error {
	s.calls = append(s.calls, name)
	if s.failNext {
		s.failNext = false
		return errors.New("boom")
	}
	return nil
}

func (s *stubController) SKI() string { return "abc123" }
func (s *stubController) Trust(ski string) error {
	s.lastTrusted = ski
	return s.maybeFail("Trust")
}
func (s *stubController) Forget(string) error { return s.maybeFail("Forget") }
func (s *stubController) SetInverterConsumptionLimit(v float64, a bool, _ float64) error {
	s.lastConsW, s.lastActive = v, a
	return s.maybeFail("SetInverterConsumptionLimit")
}
func (s *stubController) SetInverterProductionLimit(float64, bool, float64) error {
	return s.maybeFail("SetInverterProductionLimit")
}
func (s *stubController) SetEGConsumptionTarget(v float64, _ *float64) error {
	s.lastConsW = v
	return s.maybeFail("SetEGConsumptionTarget")
}
func (s *stubController) SetEGProductionTarget(float64, *float64) error {
	return s.maybeFail("SetEGProductionTarget")
}
func (s *stubController) ActivateEGConsumption(a bool) error {
	s.lastActive = a
	return s.maybeFail("ActivateEGConsumption")
}
func (s *stubController) ActivateEGProduction(bool) error {
	return s.maybeFail("ActivateEGProduction")
}
func (s *stubController) SetLPCFailsafe(*float64, *float64) error {
	return s.maybeFail("SetLPCFailsafe")
}
func (s *stubController) SetLPPFailsafe(*float64, *float64) error {
	return s.maybeFail("SetLPPFailsafe")
}
func (s *stubController) SetLPCNominalMax(float64) error { return s.maybeFail("SetLPCNominalMax") }
func (s *stubController) SetLPPNominalMax(float64) error { return s.maybeFail("SetLPPNominalMax") }

func newTestServer(token string) (*httptest.Server, *stubController, *store.Store) {
	st := store.New()
	ctrl := &stubController{}
	s := New("127.0.0.1:0", token, "test-version", st, ctrl)
	return httptest.NewServer(s.srv.Handler), ctrl, st
}

func post(t *testing.T, ts *httptest.Server, path, token, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestHealthNoAuth(t *testing.T) {
	ts, _, _ := newTestServer("secret")
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["ski"] != "abc123" || body["version"] != "test-version" {
		t.Errorf("unexpected health body: %v", body)
	}
}

func TestStateRequiresToken(t *testing.T) {
	ts, _, st := newTestServer("secret")
	defer ts.Close()
	st.Update(func(s *store.State) { s.LPC.Supported = true })

	// without token -> 401
	resp, err := ts.Client().Get(ts.URL + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token status = %d, want 401", resp.StatusCode)
	}

	// with token -> 200 and the state body
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/state", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp2, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("token status = %d, want 200", resp2.StatusCode)
	}
	var snap store.State
	_ = json.NewDecoder(resp2.Body).Decode(&snap)
	if !snap.LPC.Supported {
		t.Errorf("state body did not include store data")
	}
}

func TestLimitRoutesToController(t *testing.T) {
	ts, ctrl, _ := newTestServer("")
	defer ts.Close()

	resp := post(t, ts, "/api/lpc/limit", "", `{"value_w":4200,"active":true,"duration_s":60}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ctrl.lastConsW != 4200 || !ctrl.lastActive {
		t.Errorf("controller not called with expected args: %+v", ctrl)
	}
	if len(ctrl.calls) != 1 || ctrl.calls[0] != "SetInverterConsumptionLimit" {
		t.Errorf("unexpected calls: %v", ctrl.calls)
	}
}

func TestTargetAndActivateRoutes(t *testing.T) {
	ts, ctrl, _ := newTestServer("")
	defer ts.Close()

	post(t, ts, "/api/lpc/target", "", `{"value_w":3000}`).Body.Close()
	post(t, ts, "/api/lpc/activate", "", `{"active":true}`).Body.Close()

	if ctrl.lastConsW != 3000 || !ctrl.lastActive {
		t.Errorf("target/activate not applied: %+v", ctrl)
	}
	if !contains(ctrl.calls, "SetEGConsumptionTarget") || !contains(ctrl.calls, "ActivateEGConsumption") {
		t.Errorf("unexpected calls: %v", ctrl.calls)
	}
}

func TestTrustRoute(t *testing.T) {
	ts, ctrl, _ := newTestServer("")
	defer ts.Close()
	post(t, ts, "/api/pairing/trust", "", `{"ski":"0123456789abcdef0123456789abcdef01234567"}`).Body.Close()
	if ctrl.lastTrusted != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("trust SKI not forwarded: %q", ctrl.lastTrusted)
	}
}

func TestControllerErrorReturns502(t *testing.T) {
	ts, ctrl, _ := newTestServer("")
	defer ts.Close()
	ctrl.failNext = true

	resp := post(t, ts, "/api/lpc/failsafe", "", `{"power_w":5000}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] == nil {
		t.Errorf("expected error field in body")
	}
}

func TestBadJSONReturns400(t *testing.T) {
	ts, _, _ := newTestServer("")
	defer ts.Close()
	resp := post(t, ts, "/api/lpc/limit", "", `{"value_w": "not a number"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
