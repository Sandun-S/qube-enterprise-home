// Package configs handles loading SNMP reader configuration from SQLite.
// Analogous to v1 snmp-gateway/configs/config.go which read from configs.yml + devices.csv.
// In v2 all config is in the shared SQLite database written by conf-agent.
package configs

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"

	_ "modernc.org/sqlite"
)

// Config holds the reader-level connection and poll settings.
// Analogous to v1 SNMPCfg + poll settings from configs.yml.
type Config struct {
	ReaderID        string
	ReaderName      string
	PollIntervalSec int
	TimeoutMs       int
	Retries         int
}

// Device represents one SNMP device to poll.
// Analogous to v1 Device struct loaded from devices.csv + maps/*.csv.
type Device struct {
	SensorID  string
	Name      string
	Host      string
	Port      int
	Community string
	Version   string
	OIDs      []OIDMapping
	Tags      string // formatted "key=value,key2=value2" for core-switch
	Output    string // "influxdb", "live", "influxdb,live"
	Table     string // InfluxDB table name (e.g., "Measurements")
}

// OIDMapping maps one OID to a field key with optional scale factor.
// In v1 these came from maps/*.csv files.
type OIDMapping struct {
	OID      string
	FieldKey string
	Scale    float64
}

// OpenReadOnly opens the SQLite database in read-only WAL mode.
// Must call db.Close() when done.
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

// LoadConfigs reads reader-level settings from SQLite.
// Analogous to v1 LoadConfigs() which read from configs.yml.
func LoadConfigs(db *sql.DB, readerID string) (*Config, error) {
	var name, configJSON string
	err := db.QueryRow(
		"SELECT name, config_json FROM readers WHERE id = ? AND status = 'active'",
		readerID,
	).Scan(&name, &configJSON)
	if err != nil {
		return nil, fmt.Errorf("load reader %s: %w", readerID, err)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(configJSON), &m); err != nil {
		return nil, fmt.Errorf("parse reader config: %w", err)
	}

	// Support both naming conventions: prefer the exact field, fall back to alt name
	pollSec := getInt(m, "poll_interval_sec", 0)
	if pollSec == 0 {
		pollSec = getInt(m, "fetch_interval_sec", 30)
	}
	timeoutMs := getInt(m, "timeout_ms", 0)
	if timeoutMs == 0 {
		// timeout_sec is what the UI collects; convert to ms
		timeoutMs = getInt(m, "timeout_sec", 5) * 1000
	}
	retries := getInt(m, "retries", 0)
	if retries == 0 {
		retries = getInt(m, "worker_count", 2) // legacy alias
	}

	return &Config{
		ReaderID:        readerID,
		ReaderName:      name,
		PollIntervalSec: pollSec,
		TimeoutMs:       timeoutMs,
		Retries:         retries,
	}, nil
}

// LoadDevices reads all active sensors for this reader from SQLite.
// Analogous to v1 LoadDevices() which read from devices.csv + maps/*.csv.
func LoadDevices(db *sql.DB, readerID string) ([]Device, error) {
	rows, err := db.Query(`
		SELECT id, name, config_json, tags_json, output, table_name
		FROM sensors
		WHERE reader_id = ? AND status = 'active'
		ORDER BY name
	`, readerID)
	if err != nil {
		return nil, fmt.Errorf("query sensors: %w", err)
	}
	defer rows.Close()

	var devices []Device
	for rows.Next() {
		var sensorID, name, output, tableName string
		var configStr, tagsStr sql.NullString

		if err := rows.Scan(&sensorID, &name, &configStr, &tagsStr, &output, &tableName); err != nil {
			return nil, fmt.Errorf("scan sensor: %w", err)
		}

		var cfg map[string]any
		if configStr.Valid {
			json.Unmarshal([]byte(configStr.String), &cfg)
		}

		// Support both host and ip_address (old UI default)
		host := getString(cfg, "host", "")
		if host == "" {
			host = getString(cfg, "ip_address", "")
		}
		if host == "" {
			continue // skip sensors with no host
		}

		oids := parseOIDs(cfg)
		if len(oids) == 0 {
			continue // skip sensors with no OIDs
		}

		var tagsMap map[string]any
		if tagsStr.Valid {
			json.Unmarshal([]byte(tagsStr.String), &tagsMap)
		}

		// Support both version and snmp_version (template UI default)
		version := getString(cfg, "version", "")
		if version == "" {
			version = getString(cfg, "snmp_version", "2c")
		}

		devices = append(devices, Device{
			SensorID:  sensorID,
			Name:      name,
			Host:      host,
			Port:      getInt(cfg, "port", 161),
			Community: getString(cfg, "community", "public"),
			Version:   version,
			OIDs:      oids,
			Tags:      FormatTags(tagsMap),
			Output:    output,
			Table:     tableName,
		})
	}

	return devices, nil
}

// FormatTags converts a tag map to core-switch format: "key1=val1,key2=val2".
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

// ─── internal helpers ────────────────────────────────────────────────────────

func parseOIDs(cfg map[string]any) []OIDMapping {
	oidList, ok := cfg["oids"].([]any)
	if !ok {
		return nil
	}
	var oids []OIDMapping
	for _, o := range oidList {
		om, ok := o.(map[string]any)
		if !ok {
			continue
		}
		oid := getString(om, "oid", "")
		if oid == "" {
			continue
		}
		oids = append(oids, OIDMapping{
			OID:      oid,
			FieldKey: getString(om, "field_key", oid),
			Scale:    getFloat(om, "scale", 1.0),
		})
	}
	return oids
}

func getString(m map[string]any, key, fallback string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return fallback
}

func getInt(m map[string]any, key string, fallback int) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	if v, ok := m[key].(string); ok {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getFloat(m map[string]any, key string, fallback float64) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	if v, ok := m[key].(string); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}
