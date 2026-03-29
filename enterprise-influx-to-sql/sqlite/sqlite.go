// Package sqlite provides SQLite access for enterprise-influx-to-sql.
// Embedded from pkg/sqliteconfig — copied here so this service is self-contained
// and can live in its own repo (gitlab.com/iot-team4/product/qube/enterprise-influx-to-sql).
package sqlite

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// SensorMapping maps an InfluxDB measurement key to a cloud sensor UUID.
type SensorMapping struct {
	SensorID string
	FieldKey string
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

// LoadTelemetrySettings loads the telemetry_settings table.
// Key format: "device:reading" → SensorMapping.
func LoadTelemetrySettings(db *sql.DB) (map[string]SensorMapping, error) {
	rows, err := db.Query("SELECT device, reading, sensor_id FROM telemetry_settings")
	if err != nil {
		return nil, fmt.Errorf("query telemetry_settings: %w", err)
	}
	defer rows.Close()

	m := make(map[string]SensorMapping)
	for rows.Next() {
		var device, reading, sensorID string
		if err := rows.Scan(&device, &reading, &sensorID); err != nil {
			return nil, fmt.Errorf("scan telemetry_settings: %w", err)
		}
		key := device + ":" + reading
		m[key] = SensorMapping{
			SensorID: sensorID,
			FieldKey: reading,
		}
	}

	return m, nil
}
