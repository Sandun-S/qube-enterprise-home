// Package configs handles loading reader configuration from SQLite.
// Analogous to v1 gateway configs/config.go which read from YAML + CSV files.
// In v2 all config is in the shared SQLite database written by conf-agent.
package configs

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"

	_ "modernc.org/sqlite"
)

// ReaderConfig holds the reader-level connection/poll settings.
type ReaderConfig struct {
	ReaderID string
	Name     string
	Protocol string
	Config   map[string]any
}

// SensorConfig holds one sensor's settings.
type SensorConfig struct {
	ID     string
	Name   string
	Config map[string]any
	Tags   map[string]any
	Output string
	Table  string
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

// LoadReaderConfig reads reader and all its active sensors from SQLite.
func LoadReaderConfig(db *sql.DB, readerID string) (*ReaderConfig, []SensorConfig, error) {
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
		ReaderID: readerID,
		Name:     name,
		Protocol: protocol,
		Config:   config,
	}

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

// GetString returns a string value from a map with a fallback.
func GetString(m map[string]any, key, fallback string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return fallback
}

// GetInt returns an int value from a map with a fallback.
func GetInt(m map[string]any, key string, fallback int) int {
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
