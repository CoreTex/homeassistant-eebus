// Package eebus wires the enbility/eebus-go stack to the shared state Store. It
// runs a single local EEBUS service (a HEMS / CEM entity) that simultaneously
// acts as Controllable System (LPC/LPP receive), Energy Guard (LPC/LPP send),
// and Monitoring Appliance (MPC) plus battery (VABD) and PV (VAPD) monitoring.
package eebus

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/config"
	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"

	eebusapi "github.com/enbility/eebus-go/api"
	"github.com/enbility/eebus-go/service"
	ucapi "github.com/enbility/eebus-go/usecases/api"
	shipapi "github.com/enbility/ship-go/api"
	"github.com/enbility/ship-go/cert"
	spineapi "github.com/enbility/spine-go/api"
	"github.com/enbility/spine-go/model"
)

// Manager owns the EEBUS service and translates use-case events into Store
// updates. It implements eebusapi.ServiceReaderInterface and the logging
// interface required by the service.
type Manager struct {
	cfg     config.Config
	store   *store.Store
	logger  *Logger
	version string

	svc *service.Service

	mu          sync.Mutex
	localEntity spineapi.EntityLocalInterface
	// egEntity is the remote inverter entity we (as Energy Guard) write limits
	// to. It is discovered when the inverter announces EG-LPC/LPP support.
	egEntity spineapi.EntityRemoteInterface
	conns    map[string]*store.Connection

	// use case interfaces
	csLPC ucapi.CsLPCInterface
	csLPP ucapi.CsLPPInterface
	egLPC ucapi.EgLPCInterface
	egLPP ucapi.EgLPPInterface
	mpc   ucapi.MaMPCInterface
	vabd  ucapi.CemVABDInterface
	vapd  ucapi.CemVAPDInterface
}

// NewManager builds and sets up the EEBUS service but does not start it.
func NewManager(cfg config.Config, st *store.Store, version string) (*Manager, error) {
	m := &Manager{
		cfg:     cfg,
		store:   st,
		logger:  NewLogger(cfg.LogLevel),
		version: version,
		conns:   make(map[string]*store.Connection),
	}

	certificate, err := m.loadOrCreateCertificate()
	if err != nil {
		return nil, fmt.Errorf("certificate: %w", err)
	}

	configuration, err := eebusapi.NewConfiguration(
		cfg.DeviceBrand,  // vendor code (falls back to brand)
		cfg.DeviceBrand,  // device brand
		cfg.DeviceModel,  // device model
		cfg.DeviceSerial, // serial number
		[]shipapi.DeviceCategoryType{shipapi.DeviceCategoryTypeEnergyManagementSystem},
		model.DeviceTypeTypeEnergyManagementSystem,
		[]model.EntityTypeType{model.EntityTypeTypeCEM},
		cfg.EEBUSPort,
		certificate,
		30*time.Second, // heartbeat interval
		nil,            // no SHIP pairing announcer; we use SKI trust / auto-accept
		nil,            // in-memory SHIP digest persistence
	)
	if err != nil {
		return nil, fmt.Errorf("configuration: %w", err)
	}
	configuration.SetAlternateIdentifier(
		fmt.Sprintf("%s-%s-%s", cfg.DeviceBrand, cfg.DeviceModel, cfg.DeviceSerial))

	m.svc = service.NewService(configuration, m)
	m.svc.SetLogging(m.logger)

	if err := m.svc.Setup(); err != nil {
		return nil, fmt.Errorf("service setup: %w", err)
	}
	// SetAutoAccept must run after Setup(): the local service details it mutates
	// are only created during Setup().
	m.svc.SetAutoAccept(cfg.AutoAcceptTrust)

	m.localEntity = m.svc.LocalDevice().EntityForType(model.EntityTypeTypeCEM)
	if m.localEntity == nil {
		return nil, errors.New("local CEM entity not created")
	}

	m.setupUseCases()
	m.publishServiceInfo()

	return m, nil
}

// Start connects to pre-trusted remotes and starts the service.
func (m *Manager) Start() error {
	// Pre-trust configured remotes (grid box, inverter, ...).
	for _, ski := range m.cfg.TrustedSKIs {
		m.registerTrusted(ski)
	}
	if m.cfg.InverterSKI != "" {
		m.registerTrusted(m.cfg.InverterSKI)
	}

	if err := m.svc.Start(); err != nil {
		return err
	}
	m.publishServiceInfo()
	m.logger.Infof("EEBUS service started, local SKI: %s", m.svc.LocalService().SKI())
	return nil
}

// Shutdown stops the service.
func (m *Manager) Shutdown() {
	if m.svc != nil {
		m.svc.Shutdown()
	}
}

// SKI returns the local service SKI.
func (m *Manager) SKI() string {
	return m.svc.LocalService().SKI()
}

// Trust registers a remote SKI as trusted and connects to it.
func (m *Manager) Trust(ski string) error {
	ski = normaliseSKI(ski)
	if !validSKI(ski) {
		return fmt.Errorf("invalid SKI %q", ski)
	}
	m.registerTrusted(ski)
	return nil
}

// Forget removes trust for a remote SKI.
func (m *Manager) Forget(ski string) error {
	ski = normaliseSKI(ski)
	if !validSKI(ski) {
		return fmt.Errorf("invalid SKI %q", ski)
	}
	m.svc.UnregisterRemoteService(shipapi.NewServiceIdentity(ski, "", ""))
	m.mu.Lock()
	if c, ok := m.conns[ski]; ok {
		c.Trusted = false
		c.Connected = false
	}
	m.mu.Unlock()
	m.syncConnections()
	return nil
}

func (m *Manager) registerTrusted(ski string) {
	m.svc.RegisterRemoteService(shipapi.NewServiceIdentity(ski, "", ""))
	m.mu.Lock()
	c := m.conns[ski]
	if c == nil {
		c = &store.Connection{SKI: ski}
		m.conns[ski] = c
	}
	c.Trusted = true
	m.mu.Unlock()
	m.syncConnections()
}

// loadOrCreateCertificate loads the persisted certificate from DataDir or
// generates and persists a new one.
func (m *Manager) loadOrCreateCertificate() (tls.Certificate, error) {
	certPath := filepath.Join(m.cfg.DataDir, "cert.pem")
	keyPath := filepath.Join(m.cfg.DataDir, "key.pem")

	if fileExists(certPath) && fileExists(keyPath) {
		return tls.LoadX509KeyPair(certPath, keyPath)
	}

	certificate, err := cert.CreateCertificate(
		m.cfg.DeviceBrand, m.cfg.DeviceModel, "DE", m.cfg.DeviceSerial)
	if err != nil {
		return tls.Certificate{}, err
	}

	if err := os.MkdirAll(m.cfg.DataDir, 0o755); err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	keyBytes, err := x509.MarshalECPrivateKey(certificate.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	m.logger.Infof("generated new EEBUS certificate at %s", certPath)
	return certificate, nil
}

func (m *Manager) publishServiceInfo() {
	info := store.ServiceInfo{
		Version:    m.version,
		DeviceName: fmt.Sprintf("%s %s", m.cfg.DeviceBrand, m.cfg.DeviceModel),
	}
	if local := m.svc.LocalService(); local != nil {
		info.SKI = local.SKI()
		info.ShipID = local.ShipID()
	}
	if fp, err := m.svc.GetLocalCertificateFingerprint(); err == nil {
		info.Fingerprint = fp
	}
	if qr, err := m.svc.QRCodeText(); err == nil {
		info.QRCode = qr
	}
	m.store.Update(func(s *store.State) { s.Service = info })
}

// syncConnections writes the current connection map into the store.
func (m *Manager) syncConnections() {
	m.mu.Lock()
	list := make([]store.Connection, 0, len(m.conns))
	anyConnected := false
	for _, c := range m.conns {
		list = append(list, *c)
		if c.Connected {
			anyConnected = true
		}
	}
	m.mu.Unlock()
	m.store.Update(func(s *store.State) {
		s.Connections = list
		s.Connected = anyConnected
	})
}

func (m *Manager) upsertConn(ski string, mutate func(*store.Connection)) {
	m.mu.Lock()
	c := m.conns[ski]
	if c == nil {
		c = &store.Connection{SKI: ski}
		m.conns[ski] = c
	}
	mutate(c)
	m.mu.Unlock()
	m.syncConnections()
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
