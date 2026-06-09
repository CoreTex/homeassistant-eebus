// Package httpapi exposes the EEBUS bridge state and control endpoints over a
// small REST + WebSocket API that the Home Assistant integration consumes.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"

	"github.com/gorilla/websocket"
)

// Controller is the subset of the EEBUS manager the API needs.
type Controller interface {
	SKI() string
	Trust(ski string) error
	Forget(ski string) error
	SetInverterConsumptionLimit(valueW float64, active bool, durationS float64) error
	SetInverterProductionLimit(valueW float64, active bool, durationS float64) error
	SetEGConsumptionTarget(valueW float64, durationS *float64) error
	SetEGProductionTarget(valueW float64, durationS *float64) error
	ActivateEGConsumption(active bool) error
	ActivateEGProduction(active bool) error
	SetLPCFailsafe(powerW, durationS *float64) error
	SetLPPFailsafe(powerW, durationS *float64) error
	SetLPCNominalMax(valueW float64) error
	SetLPPNominalMax(valueW float64) error
}

// Server is the HTTP/WebSocket frontend.
type Server struct {
	store   *store.Store
	ctrl    Controller
	token   string
	version string
	srv     *http.Server
	up      websocket.Upgrader
}

// New creates a Server bound to addr.
func New(addr, token, version string, st *store.Store, ctrl Controller) *Server {
	s := &Server{
		store:   st,
		ctrl:    ctrl,
		token:   token,
		version: version,
		up: websocket.Upgrader{
			// The API is only reachable on the internal Home Assistant network,
			// so all origins are accepted.
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/state", s.auth(s.handleState))
	mux.HandleFunc("GET /api/ws", s.handleWS) // auth checked inside (query token)
	mux.HandleFunc("POST /api/lpc/limit", s.auth(s.handleLimit(limitConsumption)))
	mux.HandleFunc("POST /api/lpp/limit", s.auth(s.handleLimit(limitProduction)))
	mux.HandleFunc("POST /api/lpc/target", s.auth(s.handleTarget(limitConsumption)))
	mux.HandleFunc("POST /api/lpp/target", s.auth(s.handleTarget(limitProduction)))
	mux.HandleFunc("POST /api/lpc/activate", s.auth(s.handleActivate(limitConsumption)))
	mux.HandleFunc("POST /api/lpp/activate", s.auth(s.handleActivate(limitProduction)))
	mux.HandleFunc("POST /api/lpc/failsafe", s.auth(s.handleFailsafe(false)))
	mux.HandleFunc("POST /api/lpp/failsafe", s.auth(s.handleFailsafe(true)))
	mux.HandleFunc("POST /api/lpc/nominal_max", s.auth(s.handleNominalMax(false)))
	mux.HandleFunc("POST /api/lpp/nominal_max", s.auth(s.handleNominalMax(true)))
	mux.HandleFunc("GET /api/pairing", s.auth(s.handlePairing))
	mux.HandleFunc("POST /api/pairing/trust", s.auth(s.handleTrust))
	mux.HandleFunc("POST /api/pairing/forget", s.auth(s.handleForget))

	s.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// Start runs the HTTP server (blocking until shutdown).
func (s *Server) Start() error {
	err := s.srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// auth wraps a handler with optional bearer-token authentication.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkToken(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

func (s *Server) checkToken(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		if strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")) == s.token {
			return true
		}
	}
	// allow token via query parameter for the WebSocket handshake
	return r.URL.Query().Get("token") == s.token
}

// --- handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"ski":     s.ctrl.SKI(),
		"version": s.version,
	})
}

func (s *Server) handleState(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.Snapshot())
}

func (s *Server) handlePairing(w http.ResponseWriter, _ *http.Request) {
	snap := s.store.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"service":     snap.Service,
		"connections": snap.Connections,
	})
}

type limitKind int

const (
	limitConsumption limitKind = iota
	limitProduction
)

type limitReq struct {
	ValueW    float64 `json:"value_w"`
	Active    bool    `json:"active"`
	DurationS float64 `json:"duration_s"`
}

func (s *Server) handleLimit(kind limitKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req limitReq
		if !decode(w, r, &req) {
			return
		}
		var err error
		if kind == limitConsumption {
			err = s.ctrl.SetInverterConsumptionLimit(req.ValueW, req.Active, req.DurationS)
		} else {
			err = s.ctrl.SetInverterProductionLimit(req.ValueW, req.Active, req.DurationS)
		}
		respond(w, err)
	}
}

type targetReq struct {
	ValueW    float64  `json:"value_w"`
	DurationS *float64 `json:"duration_s"`
}

func (s *Server) handleTarget(kind limitKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req targetReq
		if !decode(w, r, &req) {
			return
		}
		var err error
		if kind == limitConsumption {
			err = s.ctrl.SetEGConsumptionTarget(req.ValueW, req.DurationS)
		} else {
			err = s.ctrl.SetEGProductionTarget(req.ValueW, req.DurationS)
		}
		respond(w, err)
	}
}

type activateReq struct {
	Active bool `json:"active"`
}

func (s *Server) handleActivate(kind limitKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req activateReq
		if !decode(w, r, &req) {
			return
		}
		var err error
		if kind == limitConsumption {
			err = s.ctrl.ActivateEGConsumption(req.Active)
		} else {
			err = s.ctrl.ActivateEGProduction(req.Active)
		}
		respond(w, err)
	}
}

type failsafeReq struct {
	PowerW    *float64 `json:"power_w"`
	DurationS *float64 `json:"duration_s"`
}

func (s *Server) handleFailsafe(production bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req failsafeReq
		if !decode(w, r, &req) {
			return
		}
		var err error
		if production {
			err = s.ctrl.SetLPPFailsafe(req.PowerW, req.DurationS)
		} else {
			err = s.ctrl.SetLPCFailsafe(req.PowerW, req.DurationS)
		}
		respond(w, err)
	}
}

type nominalReq struct {
	ValueW float64 `json:"value_w"`
}

func (s *Server) handleNominalMax(production bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req nominalReq
		if !decode(w, r, &req) {
			return
		}
		var err error
		if production {
			err = s.ctrl.SetLPPNominalMax(req.ValueW)
		} else {
			err = s.ctrl.SetLPCNominalMax(req.ValueW)
		}
		respond(w, err)
	}
}

type skiReq struct {
	SKI string `json:"ski"`
}

func (s *Server) handleTrust(w http.ResponseWriter, r *http.Request) {
	var req skiReq
	if !decode(w, r, &req) {
		return
	}
	respond(w, s.ctrl.Trust(req.SKI))
}

func (s *Server) handleForget(w http.ResponseWriter, r *http.Request) {
	var req skiReq
	if !decode(w, r, &req) {
		return
	}
	respond(w, s.ctrl.Forget(req.SKI))
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !s.checkToken(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	conn, err := s.up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	updates, unsubscribe := s.store.Subscribe()
	defer unsubscribe()

	// reader goroutine: drains control frames and detects disconnect
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case snap := <-updates:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(snap); err != nil {
				return
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// --- helpers ---

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return false
	}
	return true
}

func respond(w http.ResponseWriter, err error) {
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}
