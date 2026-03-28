// Package sqliteconfig provides functions to load reader and sensor configurations
// from the Qube edge SQLite database.
//
// All readers call these functions ONCE on startup, then close the DB connection.
// Config changes are handled by conf-agent stopping the container via Docker API;
// Swarm recreates the container which reads fresh SQLite on startup.
//
// Usage:
//
//	db := sqliteconfig.OpenReadOnly("/opt/qube/data/qube.db")
//	defer db.Close()
//	readerCfg, sensors, err := sqliteconfig.LoadReaderConfig(db, readerID)
package sqliteconfig

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// ReaderConfig holds the reader's connection settings from SQLite.
type ReaderConfig struct {
	ID       string
	Name     string
	Protocol string
	Config   map[string]any // Protocol-specific config (host, port, poll_interval, etc.)
}

// SensorConfig holds one sensor's settings from SQLite.
type SensorConfig struct {
	ID     string
	Name   string
	Config map[string]any // Protocol-specific sensor config (registers, OIDs, topics, etc.)
	Tags   map[string]any // User-defined tags
	Output string         // "influxdb", "live", "influxdb,live"
	Table  string         // InfluxDB table name (e.g., "Measurements")
}

// SensorMapping maps an InfluxDB measurement key to a cloud sensor UUID.
type SensorMapping struct {
	SensorID string
	FieldKey string
	Unit     string
}

// CoreSwitchOutputs holds the core-switch output settings.
type CoreSwitchOutputs struct {
	InfluxDB bool `json:"influxdb"`
	Live     bool `json:"live"`
}

// CoreSwitchSettings holds all core-switch settings from SQLite.
type CoreSwitchSettings struct {
	Outputs          CoreSwitchOutputs
	BatchSize        int
	FlushIntervalMs  int
}

// InfluxUpload holds one influx-to-sql upload configuration.
type InfluxUpload struct {
	Device     string
	Reading    string // "*" means all readings
	AggTimeMin int
	AggFunc    string // SUM, AVG, MAX, MIN
	ToTable    string
	TagNames   string // Pipe-separated tag dimensions
	SensorID   string
}

// OpenReadOnly opens the SQLite database in read-only WAL mode.
// The caller MUST call db.Close() when done.
func OpenReadOnly(dbPath string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL&_busy_timeout=5000", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Verify connection works
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	return db, nil
}

// LoadReaderConfig loads a reader and all its active sensors from SQLite.
// This is called ONCE on reader startup.
func LoadReaderConfig(db *sql.DB, readerID string) (*ReaderConfig, []SensorConfig, error) {
	// Load reader
	var name, protocol, configJSON string
	err := db.QueryRow(
		"SELECT name, protocol, config_json FROM readers WHERE id = ? AND status = 'active'",
		readerID,
	).Scan(&name, &protocol, &configJSON)
	if err != nil {
		return nil, nil, fmt.Errorf("load reader %s: %w", readerID, err)
	}

	var config map[string]any
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return nil, nil, fmt.Errorf("parse reader config: %w", err)
	}

	reader := &ReaderConfig{
		ID:       readerID,
		Name:     name,
		Protocol: protocol,
		Config:   config,
	}

	// Load sensors
	rows, err := db.Query(`
		SELECT id, name, config_json, tags_json, output, table_name
		FROM sensors
		WHERE reader_id = ? AND status = 'active'
		ORDER BY name
	`, readerID)
	if err != nil {
		return nil, nil, fmt.Errorf("query sensors: %w", err)
	}
	defer rows.Close()

	var sensors []SensorConfig
	for rows.Next() {
		var s SensorConfig
		var configStr, tagsStr sql.NullString
		var output, tableName string

		if err := rows.Scan(&s.ID, &s.Name, &configStr, &tagsStr, &output, &tableName); err != nil {
			return nil, nil, fmt.Errorf("scan sensor: %w", err)
		}

		s.Output = output
		s.Table = tableName

		if configStr.Valid {
			json.Unmarshal([]byte(configStr.String), &s.Config)
		}
		if tagsStr.Valid {
			json.Unmarshal([]byte(tagsStr.String), &s.Tags)
		}

		sensors = append(sensors, s)
	}

	return reader, sensors, nil
}

// LoadSensorMap loads the sensor_map table (replaces sensor_map.json).
// Used by influx-to-sql to map InfluxDB measurements to cloud sensor UUIDs.
func LoadSensorMap(db *sql.DB) (map[string]SensorMapping, error) {
	rows, err := db.Query("SELECT measurement_key, sensor_id, field_key, unit FROM sensor_map")
	if err != nil {
		return nil, fmt.Errorf("query sensor_map: %w", err)
	}
	defer rows.Close()

	m := make(map[string]SensorMapping)
	for rows.Next() {
		var key, sensorID, fieldKey string
		var unit sql.NullString
		if err := rows.Scan(&key, &sensorID, &fieldKey, &unit); err != nil {
			return nil, fmt.Errorf("scan sensor_map: %w", err)
		}
		m[key] = SensorMapping{
			SensorID: sensorID,
			FieldKey: fieldKey,
			Unit:     unit.String,
		}
	}

	return m, nil
}

// LoadCoreSwitchSettings loads core-switch settings from SQLite.
func LoadCoreSwitchSettings(db *sql.DB) (*CoreSwitchSettings, error) {
	settings := &CoreSwitchSettings{
		Outputs:         CoreSwitchOutputs{InfluxDB: true, Live: false},
		BatchSize:       100,
		FlushIntervalMs: 5000,
	}

	rows, err := db.Query("SELECT key, value_json FROM coreswitch_settings")
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

// LoadInfluxUploads loads influx-to-sql upload configurations from SQLite.
// Replaces the old uploads.csv file.
func LoadInfluxUploads(db *sql.DB) ([]InfluxUpload, error) {
	rows, err := db.Query(`
		SELECT device, reading, agg_time_min, agg_func, to_table, tag_names, sensor_id
		FROM influx_uploads
	`)
	if err != nil {
		return nil, fmt.Errorf("query influx_uploads: %w", err)
	}
	defer rows.Close()

	var uploads []InfluxUpload
	for rows.Next() {
		var u InfluxUpload
		var tagNames, sensorID sql.NullString
		if err := rows.Scan(&u.Device, &u.Reading, &u.AggTimeMin, &u.AggFunc, &u.ToTable, &tagNames, &sensorID); err != nil {
			return nil, fmt.Errorf("scan influx_upload: %w", err)
		}
		u.TagNames = tagNames.String
		u.SensorID = sensorID.String
		uploads = append(uploads, u)
	}

	return uploads, nil
}

// FormatTags converts a map of tags to core-switch format: "key1=val1,key2=val2"
func FormatTags(tags map[string]any) string {
	if len(tags) == 0 {
		return ""
	}
	result := ""
	for k, v := range tags {
		if result != "" {
			result += ","
		}
		result += fmt.Sprintf("%s=%v", k, v)
	}
	return result
}
