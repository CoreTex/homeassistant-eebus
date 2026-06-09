// Command eebus-bridge runs an EEBUS (SHIP/SPINE) service backed by
// enbility/eebus-go and exposes its state and controls over a local REST +
// WebSocket API for the Home Assistant "eebus" integration.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/config"
	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/eebus"
	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/httpapi"
	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("eebus-bridge ")

	optionsPath := flag.String("options", "/data/options.json", "path to the Home Assistant add-on options file")
	flag.Parse()

	cfg, err := config.Load(*optionsPath)
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	st := store.New()

	mgr, err := eebus.NewManager(cfg, st, version)
	if err != nil {
		log.Fatalf("eebus init error: %v", err)
	}

	if err := mgr.Start(); err != nil {
		log.Fatalf("eebus start error: %v", err)
	}
	defer mgr.Shutdown()

	// Persist a small file with the local SKI so the user can find it easily.
	writeSKIFile(cfg.DataDir, mgr.SKI())
	log.Printf("==================================================")
	log.Printf(" EEBUS Bridge ready")
	log.Printf("  Local SKI : %s", mgr.SKI())
	log.Printf("  REST/WS   : http://0.0.0.0:%d", cfg.APIPort)
	log.Printf("  Trust this SKI on your grid box / inverter to pair.")
	log.Printf("==================================================")

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.APIPort)
	api := httpapi.New(addr, cfg.APIToken, version, st, mgr)

	go func() {
		if err := api.Start(); err != nil {
			log.Fatalf("api server error: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	log.Printf("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = api.Shutdown(ctx)
}

func writeSKIFile(dataDir, ski string) {
	if dataDir == "" || ski == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(dataDir, "local_ski.txt"), []byte(ski+"\n"), 0o644)
}
