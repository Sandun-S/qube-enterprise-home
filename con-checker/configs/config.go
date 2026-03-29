// Package configs handles loading TCP connection targets for con-checker.
// Supports two config sources: SQLite connections table (v2) or CSV file (legacy).
package configs

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

// Connection represents one TCP endpoint to monitor.
type Connection struct {
	Name   string
	IPPort string
	Alert  bool
	Raised bool // tracks whether a connectivity alert is currently active
}

// OpenReadOnly opens the SQLite database in read-only WAL mode.
func OpenReadOnly(dbPath string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=journal_mode%%3DWAL&_pragma=busy_timeout%%3D5000", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return db, nil
}

// LoadFromSQLite reads connection targets from the SQLite connections table.
// Table schema:
//
//	CREATE TABLE connections (
//	    id TEXT PRIMARY KEY,
//	    name TEXT NOT NULL,
//	    host TEXT NOT NULL,
//	    port INTEGER NOT NULL,
//	    alert_enabled INTEGER NOT NULL DEFAULT 1
//	);
func LoadFromSQLite(dbPath string) ([]*Connection, error) {
	db, err := OpenReadOnly(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query("SELECT name, host, port, alert_enabled FROM connections ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("query connections: %w", err)
	}
	defer rows.Close()

	var cons []*Connection
	for rows.Next() {
		var name, host string
		var port, alertEnabled int
		if err := rows.Scan(&name, &host, &port, &alertEnabled); err != nil {
			return nil, fmt.Errorf("scan connection: %w", err)
		}
		cons = append(cons, &Connection{
			Name:   name,
			IPPort: fmt.Sprintf("%s:%d", host, port),
			Alert:  alertEnabled != 0,
		})
	}

	return cons, nil
}

// LoadFromCSV reads connection targets from a CSV file.
// Format: name,host:port,alert_enabled  (# lines are comments)
func LoadFromCSV(path string) ([]*Connection, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	var cons []*Connection
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

		cons = append(cons, &Connection{
			Name:   strings.TrimSpace(flds[0]),
			IPPort: strings.TrimSpace(flds[1]),
			Alert:  alertEnabled,
		})
	}

	return cons, nil
}
