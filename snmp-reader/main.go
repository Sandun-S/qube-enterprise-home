// snmp-reader — Qube Enterprise SNMP Reader (v2)
//
// Reads config from shared SQLite → polls SNMP OIDs from multiple devices →
// POSTs to coreswitch.
//
// SNMP is a "multi_target" reader: one reader container polls multiple devices,
// each device defined as a sensor with its own IP/community in config_json.
//
// Env vars:
//
//	READER_ID      — UUID of this reader in SQLite
//	SQLITE_PATH    — Path to shared SQLite database (read-only)
//	CORESWITCH_URL — Core-switch HTTP endpoint (default: http://core-switch:8585)
//	LOG_LEVEL      — debug, info, warn, error (default: info)
package main

import (
	"os"
	"time"

	"github.com/Sandun-S/qube-enterprise-home/snmp-reader/configs"
	"github.com/Sandun-S/qube-enterprise-home/snmp-reader/coreswitch"
	"github.com/Sandun-S/qube-enterprise-home/snmp-reader/logger"
	"github.com/Sandun-S/qube-enterprise-home/snmp-reader/snmp"
)

func main() {
	log := logger.New("snmp-reader")

	readerID := os.Getenv("READER_ID")
	sqlitePath := os.Getenv("SQLITE_PATH")
	csURL := getenv("CORESWITCH_URL", "http://core-switch:8585")

	if readerID == "" || sqlitePath == "" {
		log.Fatal("READER_ID and SQLITE_PATH are required")
	}

	// ── Load config from SQLite ──────────────────────────────────────────────
	db, err := configs.OpenReadOnly(sqlitePath)
	if err != nil {
		log.Fatalf("Failed to open SQLite: %v", err)
	}

	cfg, err := configs.LoadConfigs(db, readerID)
	if err != nil {
		log.Fatalf("Failed to load reader config: %v", err)
	}

	devices, err := configs.LoadDevices(db, readerID)
	db.Close()
	if err != nil {
		log.Fatalf("Failed to load devices: %v", err)
	}

	log.Infof("Loaded reader: name=%s sensors=%d", cfg.ReaderName, len(devices))

	if len(devices) == 0 {
		log.Warn("No sensors configured — exiting")
		return
	}

	totalOIDs := 0
	for _, d := range devices {
		totalOIDs += len(d.OIDs)
	}
	log.Infof("Polling %d devices, %d total OIDs every %ds", len(devices), totalOIDs, cfg.PollIntervalSec)

	// ── Init core-switch client and SNMP manager ─────────────────────────────
	cs := coreswitch.NewClient(csURL, "snmp-reader")
	mgr := snmp.Init(devices, cs, cfg.ReaderName, cfg.TimeoutMs, cfg.Retries, log)

	// ── Poll loop ────────────────────────────────────────────────────────────
	ticker := time.NewTicker(time.Duration(cfg.PollIntervalSec) * time.Second)
	defer ticker.Stop()

	mgr.Trigger() // poll immediately on startup
	for range ticker.C {
		mgr.Trigger()
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
