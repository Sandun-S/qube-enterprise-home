// con-checker — Qube Enterprise TCP Connectivity Checker (v2)
//
// Periodically checks TCP connectivity to configured endpoints.
// Sends alerts to core-switch when a connection fails or is restored.
//
// Config source priority:
//  1. SQLite connections table (when SQLITE_PATH is set)
//  2. CSV file (CONNECTIONS_FILE env var, or default connections.csv)
//
// CSV format (one per line): name,host:port,alert_enabled
//
//	# comment lines are skipped
//	PLC_Rack_A,192.168.1.50:502,true
//	UPS_Main,10.0.0.5:161,false
//
// Env vars:
//
//	SQLITE_PATH       — Path to edge SQLite database (read connections from DB)
//	CONNECTIONS_FILE  — CSV file path [default: connections.csv]
//	CORESWITCH_URL    — Core-switch alerts endpoint [default: http://core-switch:8585]
//	INTERVAL_SEC      — Check interval in seconds [default: 10]
//	TIMEOUT_SEC       — TCP dial timeout in seconds [default: 2]
//	LOG_LEVEL         — debug, info, warn, error [default: info]
package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/Sandun-S/qube-enterprise-home/con-checker/configs"
	"github.com/Sandun-S/qube-enterprise-home/con-checker/coreswitch"
	"github.com/Sandun-S/qube-enterprise-home/con-checker/logger"
)

func main() {
	log := logger.New("con-checker")

	csURL := getenv("CORESWITCH_URL", "http://core-switch:8585")
	intervalSec := envInt("INTERVAL_SEC", 10)
	timeoutSec := envInt("TIMEOUT_SEC", 2)

	cs := coreswitch.NewClient(csURL, "con-checker")

	log.Infof("Starting con-checker: interval=%ds timeout=%ds coreswitch=%s",
		intervalSec, timeoutSec, csURL)

	// ── Load connections ──────────────────────────────────────────────────────
	cons := loadConnections(log)

	if len(cons) == 0 {
		log.Warn("No connections configured — nothing to check")
	} else {
		log.Infof("Loaded %d connection(s) to monitor", len(cons))
	}

	timeout := time.Duration(timeoutSec) * time.Second
	interval := time.Duration(intervalSec) * time.Second

	// ── Check loop ────────────────────────────────────────────────────────────
	for {
		for _, con := range cons {
			conn, err := net.DialTimeout("tcp", con.IPPort, timeout)

			if err != nil {
				msg := fmt.Sprintf("Cannot connect to %s on %s - %s", con.Name, con.IPPort, err.Error())
				log.Infof("FAIL: %s", msg)

				if con.Alert && !con.Raised {
					cs.SendAlert("Connectivity", msg, 1)
				}
				con.Raised = true

			} else {
				conn.Close()

				if con.Raised {
					msg := fmt.Sprintf("Connection to %s on %s established", con.Name, con.IPPort)
					log.Infof("OK: %s", msg)

					if con.Alert {
						cs.SendAlert("Connectivity", msg, 0)
					}
					con.Raised = false
				} else {
					log.Debugf("OK: %s (%s)", con.Name, con.IPPort)
				}
			}
		}

		time.Sleep(interval)
	}
}

// loadConnections loads from SQLite (if SQLITE_PATH set) or falls back to CSV.
func loadConnections(log interface {
	Infof(string, ...any)
	Warnf(string, ...any)
	Fatalf(string, ...any)
}) []*configs.Connection {
	sqlitePath := os.Getenv("SQLITE_PATH")
	if sqlitePath != "" {
		cons, err := configs.LoadFromSQLite(sqlitePath)
		if err != nil {
			log.Warnf("SQLite load failed (%v), falling back to CSV", err)
		} else {
			return cons
		}
	}

	csvPath := getenv("CONNECTIONS_FILE", "connections.csv")
	cons, err := configs.LoadFromCSV(csvPath)
	if err != nil {
		log.Fatalf("Cannot load connections from %s: %v", csvPath, err)
	}
	return cons
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
