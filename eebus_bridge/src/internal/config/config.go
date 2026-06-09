// Package config loads the add-on configuration from the Home Assistant
// add-on options file (/data/options.json) and applies sane defaults so the
// service can also be run stand-alone for development.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Config holds all runtime configuration. Every value maps 1:1 to an option in
// the add-on config.yaml schema. Nothing in here is specific to a particular
// network, vendor or device - everything is supplied by the user at runtime.
type Config struct {
	// Logging
	LogLevel string `json:"log_level"`

	// Local REST/WebSocket API exposed to the Home Assistant integration
	APIPort  int    `json:"api_port"`
	APIToken string `json:"api_token"`

	// EEBUS / SHIP identity of this service (the "HEMS" the user runs)
	EEBUSPort    int    `json:"eebus_port"`
	DeviceBrand  string `json:"device_brand"`
	DeviceModel  string `json:"device_model"`
	DeviceSerial string `json:"device_serial"`

	// Which EEBUS use cases / roles to enable
	EnableLPC       bool `json:"enable_lpc"`        // Controllable System: receive consumption limits
	EnableLPP       bool `json:"enable_lpp"`        // Controllable System: receive production limits
	EnableMPC       bool `json:"enable_mpc"`        // Monitoring of Power Consumption
	EnableBattery   bool `json:"enable_battery"`    // VABD ~ Monitoring of Battery (MOB)
	EnableInverter  bool `json:"enable_inverter"`   // VAPD ~ Monitoring of Inverter (MOI)
	EnableEGControl bool `json:"enable_eg_control"` // Energy Guard: send limits to the inverter

	// Optional nominal maximum power seeds for the Controllable System, in watts.
	// 0 means "leave unset" (the value can still be changed at runtime via the
	// number entities / API). These persist the §14a-relevant nominal maxima
	// across add-on restarts.
	LpcNominalMaxW float64 `json:"lpc_nominal_max_w"`
	LppNominalMaxW float64 `json:"lpp_nominal_max_w"`

	// Trust / pairing behaviour
	AutoAcceptTrust   bool     `json:"auto_accept_trust"`   // auto-trust any remote that wants to pair
	AutoApproveLimits bool     `json:"auto_approve_limits"` // auto-accept incoming LPC/LPP write limits
	TrustedSKIs       []string `json:"trusted_skis"`        // pre-trusted remote SKIs (e.g. grid box, inverter)
	InverterSKI       string   `json:"inverter_ski"`        // optional target SKI for EG control / monitoring

	// Where to persist certificate, key and SHIP state. Defaults to /data.
	DataDir string `json:"-"`
}

// Default returns a Config pre-populated with reasonable defaults.
func Default() Config {
	return Config{
		LogLevel:          "info",
		APIPort:           7050,
		APIToken:          "",
		EEBUSPort:         4715,
		DeviceBrand:       "HomeAssistant",
		DeviceModel:       "EEBUS-Bridge",
		DeviceSerial:      "",
		EnableLPC:         true,
		EnableLPP:         true,
		EnableMPC:         true,
		EnableBattery:     true,
		EnableInverter:    true,
		EnableEGControl:   false,
		LpcNominalMaxW:    0,
		LppNominalMaxW:    0,
		AutoAcceptTrust:   false,
		AutoApproveLimits: true,
		TrustedSKIs:       nil,
		InverterSKI:       "",
		DataDir:           "/data",
	}
}

// Load reads the options JSON file at path (typically /data/options.json),
// overlays it onto the defaults and validates the result. A missing file is not
// an error: the defaults are returned so the binary can run for development.
func Load(path string) (Config, error) {
	cfg := Default()
	if dir := os.Getenv("EEBUS_DATA_DIR"); dir != "" {
		cfg.DataDir = dir
	}

	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		// no options file - run with defaults (dev / stand-alone mode)
	case err != nil:
		return cfg, fmt.Errorf("reading options file %q: %w", path, err)
	default:
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parsing options file %q: %w", path, err)
		}
	}

	cfg.normalise()
	if err := cfg.validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c *Config) normalise() {
	c.LogLevel = strings.ToLower(strings.TrimSpace(c.LogLevel))
	c.InverterSKI = normaliseSKI(c.InverterSKI)
	for i, ski := range c.TrustedSKIs {
		c.TrustedSKIs[i] = normaliseSKI(ski)
	}
	if c.DeviceSerial == "" {
		// A stable-ish default serial derived from the host so two installs on
		// the same network do not collide. Users may override it.
		if host, err := os.Hostname(); err == nil && host != "" {
			c.DeviceSerial = "ha-" + sanitise(host)
		} else {
			c.DeviceSerial = "ha-eebus-bridge"
		}
	}
}

func (c *Config) validate() error {
	if c.APIPort <= 0 || c.APIPort > 65535 {
		return fmt.Errorf("api_port %d out of range", c.APIPort)
	}
	if c.EEBUSPort <= 0 || c.EEBUSPort > 65535 {
		return fmt.Errorf("eebus_port %d out of range", c.EEBUSPort)
	}
	if c.APIPort == c.EEBUSPort {
		return fmt.Errorf("api_port and eebus_port must differ (both %d)", c.APIPort)
	}
	for _, ski := range c.TrustedSKIs {
		if !validSKI(ski) {
			return fmt.Errorf("trusted_skis contains invalid SKI %q (expected 40 hex chars)", ski)
		}
	}
	if c.InverterSKI != "" && !validSKI(c.InverterSKI) {
		return fmt.Errorf("inverter_ski %q is not a valid SKI (expected 40 hex chars)", c.InverterSKI)
	}
	return nil
}

// normaliseSKI lower-cases and strips separators/whitespace commonly found when
// users copy a SKI out of another tool.
func normaliseSKI(ski string) string {
	ski = strings.ToLower(strings.TrimSpace(ski))
	ski = strings.NewReplacer(" ", "", ":", "", "-", "").Replace(ski)
	return ski
}

func validSKI(ski string) bool {
	if len(ski) != 40 {
		return false
	}
	for _, r := range ski {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !isHex {
			return false
		}
	}
	return true
}

func sanitise(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 24 {
		out = out[:24]
	}
	return out
}
