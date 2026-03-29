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
//   # comment lines are skipped
//   PLC_Rack_A,192.168.1.50:502,true
//   UPS_Main,10.0.0.5:161,false
//
// Env vars:
//   SQLITE_PATH       — Path to edge SQLite database (read connections from DB)
//   CONNECTIONS_FILE  — CSV file path [default: connections.csv]
//   CORESWITCH_URL    — Core-switch alerts endpoint [default: http://core-switch:8585]
//   INTERVAL_SEC      — Check interval in seconds [default: 10]
//   TIMEOUT_SEC       — TCP dial timeout in seconds [default: 2]
//   LOG_LEVEL         — debug, info, warn, error [default: info]
package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/qube-enterprise/pkg/coreswitch"
	"github.com/qube-enterprise/pkg/logger"
	_ "modernc.org/sqlite"
)

type connection struct {
	Name    string
	IPPort  string
	Alert   bool
	raised  bool // tracks whether an alert is currently active
}

func main() {
	log := logger.New("con-checker")

	csURL := getenv("CORESWITCH_URL", "http://core-switch:8585")
	intervalSec := envInt("INTERVAL_SEC", 10)
	timeoutSec := envInt("TIMEOUT_SEC", 2)

	cs := coreswitch.NewClient(csURL, "con-checker")

	log.Infof("Starting con-checker: interval=%ds timeout=%ds coreswitch=%s",
		intervalSec, timeoutSec, csURL)

	// Load connections
	cons := loadConnections(log)

	if len(cons) == 0 {
		log.Warn("No connections configured — nothing to check")
	} else {
		log.Infof("Loaded %d connection(s) to monitor", len(cons))
	}

	timeout := time.Duration(timeoutSec) * time.Second
	interval := time.Duration(intervalSec) * time.Second

	for {
		for _, con := range cons {
			conn, err := net.DialTimeout("tcp", con.IPPort, timeout)

			if err != nil {
				msg := fmt.Sprintf("Cannot connect to %s on %s - %s", con.Name, con.IPPort, err.Error())
				log.Infof("FAIL: %s", msg)

				if con.Alert && !con.raised {
					cs.SendAlert("Connectivity", msg, 1)
				}
				con.raised = true

			} else {
				conn.Close()

				if con.raised {
					msg := fmt.Sprintf("Connection to %s on %s established", con.Name, con.IPPort)
					log.Infof("OK: %s", msg)

					if con.Alert {
						cs.SendAlert("Connectivity", msg, 0)
					}
					con.raised = false
				} else {
					log.Debugf("OK: %s (%s)", con.Name, con.IPPort)
				}
			}
		}

		time.Sleep(interval)
	}
}

// loadConnections loads connections from SQLite (if SQLITE_PATH set) or CSV file.
func loadConnections(log interface{ Infof(string, ...any); Warnf(string, ...any); Fatalf(string, ...any) }) []*connection {
	sqlitePath := os.Getenv("SQLITE_PATH")
	if sqlitePath != "" {
		cons, err := loadFromSQLite(sqlitePath)
		if err != nil {
			log.Warnf("SQLite load failed (%v), falling back to CSV", err)
		} else {
			return cons
		}
	}

	csvPath := getenv("CONNECTIONS_FILE", "connections.csv")
	cons, err := loadFromCSV(csvPath)
	if err != nil {
		log.Fatalf("Cannot load connections from %s: %v", csvPath, err)
	}
	return cons
}

// loadFromSQLite reads connections from the SQLite connections table.
// Table schema:
//
//	CREATE TABLE connections (
//	    id TEXT PRIMARY KEY,
//	    name TEXT NOT NULL,
//	    host TEXT NOT NULL,
//	    port INTEGER NOT NULL,
//	    alert_enabled INTEGER NOT NULL DEFAULT 1
//	);
func loadFromSQLite(dbPath string) ([]*connection, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=journal_mode%%3DWAL&_pragma=busy_timeout%%3D5000", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT name, host, port, alert_enabled FROM connections ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("query connections: %w", err)
	}
	defer rows.Close()

	var cons []*connection
	for rows.Next() {
		var name, host string
		var port, alertEnabled int
		if err := rows.Scan(&name, &host, &port, &alertEnabled); err != nil {
			return nil, fmt.Errorf("scan connection: %w", err)
		}
		cons = append(cons, &connection{
			Name:   name,
			IPPort: fmt.Sprintf("%s:%d", host, port),
			Alert:  alertEnabled != 0,
		})
	}

	return cons, nil
}

// loadFromCSV reads connections from a CSV file.
// Format: name,host:port,alert_enabled (# lines are comments)
func loadFromCSV(path string) ([]*connection, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	var cons []*connection
	scanner := bufio.NewScanner(fd)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' {
			continue
		}

		flds := strings.SplitN(line, ",", 3)
		if len(flds) < 2 {
			continue
		}

		alertEnabled := true
		if len(flds) >= 3 {
			alertEnabled = strings.TrimSpace(flds[2]) == "true"
		}

		cons = append(cons, &connection{
			Name:   strings.TrimSpace(flds[0]),
			IPPort: strings.TrimSpace(flds[1]),
			Alert:  alertEnabled,
		})
	}

	return cons, nil
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
