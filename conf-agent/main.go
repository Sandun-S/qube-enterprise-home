// conf-agent v2 — Qube Enterprise Edge Agent
//
// Combines the original conf-agent-master local HTTP server (web UI, maintenance,
// device controls) with the enterprise cloud sync stack (WebSocket config push,
// TP-API HTTP polling fallback, SQLite config store, Docker Swarm deploy).
//
// Startup flow:
//  1. Read config.yml + env var overrides
//  2. Read /boot/mit.txt → device_id, register_key, maintain_key
//  3. Start local HTTP management server (web UI on :Port)
//  4. Wait for TP-API to be reachable
//  5. Self-register if QUBE_TOKEN not set → get auth token
//  6. Initialize SQLite database
//  7. Start enterprise agent: WebSocket (primary) + HTTP polling (fallback)
//
// Config sources (in priority order):
//   env vars > config.yml > built-in defaults
//
// Key env vars:
//
//	CLOUD_WS_URL   — WebSocket URL     [default: ws://localhost:8080/ws]
//	TPAPI_URL      — TP-API base URL   [default: http://localhost:8081]
//	QUBE_ID        — Qube ID           (overrides mit.txt)
//	QUBE_TOKEN     — Auth token        (auto-obtained via self-registration)
//	REGISTER_KEY   — Registration key  (overrides mit.txt)
//	SQLITE_PATH    — SQLite path       [default: /opt/qube/data/qube.db]
//	WORK_DIR       — Working directory [default: /opt/qube]
//	POLL_INTERVAL  — Poll interval (s) [default: 30]
//	MIT_TXT_PATH   — Device identity   [default: /boot/mit.txt]
package main

import (
	"database/sql"
	"flag"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	_ "modernc.org/sqlite"

	"conf-agent/agent"
	"conf-agent/configs"
	"conf-agent/http"
	"conf-agent/sqlite"
	"conf-agent/tpapi"

	"github.com/sirupsen/logrus"
)

var logger *logrus.Logger

// ============================================================================
func main() {
	confFile := flag.String("config", "config.yml", "Configuration file for conf-agent")
	folder := flag.String("dir", ".", "Directory the service should run from")
	flag.Parse()

	// ── Initialize logrus (same setup as v1 conf-agent-master) ───────────────
	logger = logrus.New()
	logger.Formatter = new(logrus.TextFormatter)
	logger.Formatter.(*logrus.TextFormatter).FullTimestamp = true
	logger.Formatter.(*logrus.TextFormatter).CallerPrettyfier = func(frame *runtime.Frame) (function string, file string) {
		fileName := path.Base(frame.File) + ":" + strconv.Itoa(frame.Line)
		return function, fileName
	}
	logger.SetReportCaller(true)
	logger.Print("[main] Starting conf-agent v2 in folder ", *folder)

	os.Chdir(*folder)

	// ── Step 1: Load configuration ────────────────────────────────────────────
	conf := configs.LoadConfigs(logger, *confFile)

	// ── Step 2: Read device identity from /boot/mit.txt ──────────────────────
	var mit *configs.MitTxt
	mit, err := configs.ReadMitTxt(conf.MitTxtPath)
	if err != nil {
		logger.Warnf("[main] Could not read %s: %v — using env vars", conf.MitTxtPath, err)
	} else {
		logger.Infof("[main] Device identity: id=%s name=%s type=%s",
			mit.DeviceID, mit.DeviceName, mit.DeviceType)
		if conf.QubeID == "" {
			conf.QubeID = mit.DeviceID
		}
		if conf.RegisterKey == "" {
			conf.RegisterKey = mit.RegisterKey
		}
	}

	if conf.QubeID == "" {
		logger.Fatal("[main] Cannot determine device ID. Set QUBE_ID or ensure /boot/mit.txt exists.")
	}

	// ── Step 3: Start local HTTP management server (web UI, maintenance) ─────
	// Runs in background goroutine — same as v1 conf-agent-master.
	// Provides: /, /reboot, /shutdown, /reset-ips, /repair, /logs, /backup
	http.Init(logger, conf, mit)

	// ── Step 4: Wait for TP-API ───────────────────────────────────────────────
	logger.Printf("[main] QubeID=%s TPAPI=%s WS=%s PollInterval=%s",
		conf.QubeID, conf.TPAPIURL, conf.CloudWSURL, conf.PollInterval)

	bootstrapClient := tpapi.NewClient(conf.TPAPIURL, conf.QubeID, conf.QubeToken)
	for {
		_, status, err := bootstrapClient.Do("GET", "/health", nil)
		if err == nil && status == 200 {
			logger.Println("[main] TP-API reachable")
			break
		}
		logger.Printf("[main] TP-API not reachable (err=%v status=%d) — retrying in 10s", err, status)
		time.Sleep(10 * time.Second)
	}

	// ── Step 5: Self-register if no token ─────────────────────────────────────
	if conf.QubeToken == "" {
		logger.Println("[main] No QUBE_TOKEN — attempting self-registration...")
		conf.QubeToken = agent.SelfRegister(bootstrapClient, conf.QubeID, conf.RegisterKey, conf.WorkDir)
	}
	if conf.QubeToken == "" {
		logger.Fatal("[main] Could not obtain QUBE_TOKEN. Device may not be claimed yet.")
	}

	// Expose token + IDs as env vars so docker stack deploy substitutes them
	// in the generated docker-compose.yml (${QUBE_TOKEN}, ${QUBE_ID}, ${TPAPI_URL}).
	os.Setenv("QUBE_TOKEN", conf.QubeToken)
	os.Setenv("QUBE_ID", conf.QubeID)
	os.Setenv("TPAPI_URL", conf.TPAPIURL)

	// ── Step 6: Initialize SQLite ─────────────────────────────────────────────
	if err := os.MkdirAll(filepath.Dir(conf.SQLitePath), 0755); err != nil {
		logger.Fatalf("[main] Cannot create SQLite directory: %v", err)
	}
	db, err := sql.Open("sqlite", conf.SQLitePath)
	if err != nil {
		logger.Fatalf("[main] Cannot open SQLite: %v", err)
	}
	defer db.Close()
	sqlite.Init(db)
	logger.Printf("[main] SQLite initialized at %s", conf.SQLitePath)

	// ── Step 7: Start enterprise agent (blocking) ─────────────────────────────
	// WebSocket primary + HTTP polling fallback + config sync + command dispatch
	log.Printf("[main] Starting enterprise agent...")
	a := agent.New(conf, db, logger)
	a.Start()
}
