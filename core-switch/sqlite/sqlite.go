// Package sqlite provides SQLite access for core-switch settings.
// Embedded from pkg/sqliteconfig — copied here so core-switch is self-contained
// and can live in its own repo (gitlab.com/iot-team4/product/qube/core-switch).
package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

// CoreSwitchOutputs holds the core-switch output settings.
type CoreSwitchOutputs struct {
	InfluxDB bool `json:"influxdb"`
	Live     bool `json:"live"`
}

// CoreSwitchSettings holds all core-switch settings loaded from SQLite.
type CoreSwitchSettings struct {
	Outputs         CoreSwitchOutputs
	BatchSize       int
	FlushIntervalMs int
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

// LoadCoreSwitchSettings loads core-switch settings from the coreswitch_settings table.
// Returns defaults if the table doesn't exist or settings are missing.
func LoadCoreSwitchSettings(db *sql.DB) (*CoreSwitchSettings, error) {
	settings := &CoreSwitchSettings{
		Outputs:         CoreSwitchOutputs{InfluxDB: true, Live: false},
		BatchSize:       100,
		FlushIntervalMs: 5000,
	}

	rows, err := db.Query("SELECT key, value FROM coreswitch_settings")
	if err != nil {
		return settings, nil // return defaults if table doesn't exist yet
	}
	defer rows.Close()

	for rows.Next() {
		var key, valueJSON string
		if err := rows.Scan(&key, &valueJSON); err != nil {
			continue
		}
		switch key {
		case "outputs":
			json.Unmarshal([]byte(valueJSON), &settings.Outputs)
		case "batch_size":
			json.Unmarshal([]byte(valueJSON), &settings.BatchSize)
		case "flush_interval_ms":
			json.Unmarshal([]byte(valueJSON), &settings.FlushIntervalMs)
		}
	}

	return settings, nil
}
