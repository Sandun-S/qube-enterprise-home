// conf-agent v2 — Qube Enterprise Edge Agent
//
// Primary: WebSocket connection to cloud (real-time config push + commands)
// Fallback: HTTP polling to TP-API (when WS disconnected)
//
// Startup flow:
//  1. Read /boot/mit.txt → get device_id + register_key
//  2. If QUBE_TOKEN not set → call /v1/device/register (HTTP polling)
//  3. Connect WebSocket to cloud (ws://<cloud>:8080/ws?qube_id=X&token=Y)
//  4. On WS: receive config_push + commands in real-time
//  5. Fallback: poll TP-API every 30s if WebSocket is disconnected
//  6. Config stored in SQLite at SQLITE_PATH (shared with reader containers)
//
// Env vars:
//
//	CLOUD_WS_URL   — WebSocket URL [default: ws://localhost:8080/ws]
//	TPAPI_URL      — TP-API base URL [default: http://localhost:8081]
//	QUBE_ID        — Qube ID (overrides mit.txt)
//	QUBE_TOKEN     — Auth token (auto-obtained via self-registration)
//	REGISTER_KEY   — Registration key (overrides mit.txt)
//	SQLITE_PATH    — SQLite path [default: /opt/qube/data/qube.db]
//	WORK_DIR       — Working directory [default: /opt/qube]
//	POLL_INTERVAL  — Poll interval in seconds [default: 30]
//	MIT_TXT_PATH   — Device identity file [default: /boot/mit.txt]
package main

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Sandun-S/qube-enterprise-home/conf-agent/agent"
	"github.com/Sandun-S/qube-enterprise-home/conf-agent/configs"
	"github.com/Sandun-S/qube-enterprise-home/conf-agent/sqlite"
	"github.com/Sandun-S/qube-enterprise-home/conf-agent/tpapi"
)

func main() {
	cfg := configs.LoadConfig()

	// ── Step 1: Read /boot/mit.txt ────────────────────────────────────────────
	mit, err := configs.ReadMitTxt(cfg.MitTxtPath)
	if err != nil {
		log.Printf("[agent] Could not read %s: %v", cfg.MitTxtPath, err)
		log.Printf("[agent] Falling back to env vars (QUBE_ID, REGISTER_KEY)")
	} else {
		log.Printf("[agent] Device identity from mit.txt: id=%s reg=%s type=%s",
			mit.DeviceID, mit.RegisterKey, mit.DeviceType)
		if cfg.QubeID == "" {
			cfg.QubeID = mit.DeviceID
		}
		if cfg.RegisterKey == "" {
			cfg.RegisterKey = mit.RegisterKey
		}
	}

	if cfg.QubeID == "" {
		log.Fatal("[agent] Cannot determine device ID. Set QUBE_ID or ensure /boot/mit.txt exists.")
	}

	// ── Step 2: Wait for TP-API ───────────────────────────────────────────────
	log.Printf("[agent] Starting v2 — QubeID=%s TPAPI=%s WS=%s Interval=%s",
		cfg.QubeID, cfg.TPAPIURL, cfg.CloudWSURL, cfg.PollInterval)

	bootstrapClient := tpapi.NewClient(cfg.TPAPIURL, cfg.QubeID, cfg.QubeToken)
	for {
		_, status, err := bootstrapClient.Do("GET", "/health", nil)
		if err == nil && status == 200 {
			log.Println("[agent] TP-API reachable")
			break
		}
		log.Printf("[agent] TP-API not reachable (err=%v status=%d), retrying in 10s...", err, status)
		time.Sleep(10 * time.Second)
	}

	// ── Step 3: Self-register if no token ─────────────────────────────────────
	if cfg.QubeToken == "" {
		log.Println("[agent] No QUBE_TOKEN — attempting self-registration...")
		cfg.QubeToken = agent.SelfRegister(bootstrapClient, cfg.QubeID, cfg.RegisterKey, cfg.WorkDir)
	}
	if cfg.QubeToken == "" {
		log.Fatal("[agent] Could not obtain QUBE_TOKEN. Device may not be claimed yet.")
	}

	// ── Step 4: Initialize SQLite ─────────────────────────────────────────────
	if err := os.MkdirAll(filepath.Dir(cfg.SQLitePath), 0755); err != nil {
		log.Fatalf("[agent] Cannot create SQLite directory: %v", err)
	}
	db, err := sql.Open("sqlite", cfg.SQLitePath)
	if err != nil {
		log.Fatalf("[agent] Cannot open SQLite: %v", err)
	}
	defer db.Close()
	sqlite.Init(db)
	log.Printf("[agent] SQLite initialized at %s", cfg.SQLitePath)

	// ── Step 5: Create agent (restores last known hash from disk) ───────────────
	a := agent.New(cfg, db)

	// ── Step 6: Start WebSocket + polling loop ────────────────────────────────
	a.Start()
}
