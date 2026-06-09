package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APIPort != 7050 {
		t.Errorf("APIPort = %d, want 7050", cfg.APIPort)
	}
	if !cfg.EnableLPC || !cfg.EnableMPC {
		t.Errorf("expected LPC and MPC enabled by default")
	}
	if cfg.DeviceSerial == "" {
		t.Errorf("expected a generated device serial")
	}
}

func TestLoadOverlaysFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "options.json")
	content := `{
		"api_port": 8123,
		"enable_eg_control": true,
		"inverter_ski": "AB:CD ef0123456789012345678901234567890123",
		"trusted_skis": ["0123456789abcdef0123456789abcdef01234567"]
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APIPort != 8123 {
		t.Errorf("APIPort = %d, want 8123", cfg.APIPort)
	}
	if !cfg.EnableEGControl {
		t.Errorf("expected EG control enabled from file")
	}
	if cfg.InverterSKI != "abcdef0123456789012345678901234567890123" {
		t.Errorf("inverter SKI not normalised: %q", cfg.InverterSKI)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"default ok", func(*Config) {}, false},
		{"api port zero", func(c *Config) { c.APIPort = 0 }, true},
		{"api port high", func(c *Config) { c.APIPort = 70000 }, true},
		{"same ports", func(c *Config) { c.EEBUSPort = c.APIPort }, true},
		{"bad trusted ski", func(c *Config) { c.TrustedSKIs = []string{"xyz"} }, true},
		{"bad inverter ski", func(c *Config) { c.InverterSKI = "nothex" }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(&cfg)
			err := cfg.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNormaliseSKI(t *testing.T) {
	got := normaliseSKI("  AB:CD-ef 01  ")
	if got != "abcdef01" {
		t.Errorf("normaliseSKI = %q, want abcdef01", got)
	}
}

func TestValidSKI(t *testing.T) {
	if !validSKI("0123456789abcdef0123456789abcdef01234567") {
		t.Errorf("expected valid SKI to pass")
	}
	if validSKI("tooshort") {
		t.Errorf("expected short SKI to fail")
	}
	if validSKI("0123456789abcdef0123456789abcdef0123456g") {
		t.Errorf("expected non-hex SKI to fail")
	}
}
