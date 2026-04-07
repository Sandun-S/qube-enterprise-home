// Package sqlite handles SQLite schema initialization and config writes for conf-agent.
// The SQLite database is the shared config store read by all reader containers.
package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/Sandun-S/qube-enterprise-home/conf-agent/tpapi"
)

// Init creates the SQLite schema if it doesn't exist.
// Called once on agent startup after opening the database.
func Init(db *sql.DB) {
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")

	db.Exec(`CREATE TABLE IF NOT EXISTS readers (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL,
		protocol    TEXT NOT NULL,
		config_json TEXT NOT NULL DEFAULT '{}',
		status      TEXT NOT NULL DEFAULT 'active'
	)`)

	db.Exec(`CREATE TABLE IF NOT EXISTS sensors (
		id          TEXT PRIMARY KEY,
		reader_id   TEXT NOT NULL REFERENCES readers(id),
		name        TEXT NOT NULL,
		config_json TEXT NOT NULL DEFAULT '{}',
		tags_json   TEXT NOT NULL DEFAULT '{}',
		output      TEXT NOT NULL DEFAULT 'influxdb',
		table_name  TEXT NOT NULL DEFAULT ''
	)`)

	db.Exec(`CREATE TABLE IF NOT EXISTS coreswitch_settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`)

	db.Exec(`CREATE TABLE IF NOT EXISTS telemetry_settings (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		device       TEXT NOT NULL,
		reading      TEXT NOT NULL,
		agg_time_min INTEGER NOT NULL DEFAULT 1,
		agg_func     TEXT NOT NULL DEFAULT 'last',
		sensor_id    TEXT NOT NULL,
		tag_names    TEXT NOT NULL DEFAULT '[]'
	)`)

	db.Exec(`CREATE TABLE IF NOT EXISTS meta (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`)
}

// WriteConfig writes the full sync config into SQLite atomically.
// Called every time a new config is downloaded from the cloud.
func WriteConfig(db *sql.DB, sc *tpapi.SyncConfig) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Clear existing data
	tx.Exec("DELETE FROM sensors")
	tx.Exec("DELETE FROM readers")
	tx.Exec("DELETE FROM coreswitch_settings")
	tx.Exec("DELETE FROM telemetry_settings")

	// Write readers + sensors
	for _, rd := range sc.Readers {
		cfgJSON, _ := json.Marshal(rd.ConfigJSON)
		status := rd.Status
		if status == "" {
			status = "active"
		}
		tx.Exec(`INSERT INTO readers (id, name, protocol, config_json, status) VALUES (?, ?, ?, ?, ?)`,
			rd.ID, rd.Name, rd.Protocol, string(cfgJSON), status)

		for _, s := range rd.Sensors {
			sCfgJSON, _ := json.Marshal(s.ConfigJSON)
			sTagsJSON, _ := json.Marshal(s.TagsJSON)
			tx.Exec(`INSERT INTO sensors (id, reader_id, name, config_json, tags_json, output, table_name)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				s.ID, rd.ID, s.Name, string(sCfgJSON), string(sTagsJSON), s.Output, s.TableName)
		}
	}

	// Write coreswitch settings
	for k, v := range sc.CoreSwitchSettings {
		tx.Exec(`INSERT INTO coreswitch_settings (key, value) VALUES (?, ?)`, k, v)
	}

	// Write telemetry settings
	for _, ts := range sc.TelemetrySettings {
		device, _ := ts["device"].(string)
		reading, _ := ts["reading"].(string)
		aggTimeMin := 1
		if v, ok := ts["agg_time_min"].(float64); ok {
			aggTimeMin = int(v)
		}
		aggFunc, _ := ts["agg_func"].(string)
		if aggFunc == "" {
			aggFunc = "last"
		}
		sensorID, _ := ts["sensor_id"].(string)
		tagNamesJSON := "[]"
		if tn, ok := ts["tag_names"]; ok {
			b, _ := json.Marshal(tn)
			tagNamesJSON = string(b)
		}
		tx.Exec(`INSERT INTO telemetry_settings (device, reading, agg_time_min, agg_func, sensor_id, tag_names)
			VALUES (?, ?, ?, ?, ?, ?)`,
			device, reading, aggTimeMin, aggFunc, sensorID, tagNamesJSON)
	}

	// Write meta
	tx.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('config_hash', ?)`, sc.Hash)
	tx.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('config_version', ?)`,
		strconv.Itoa(sc.ConfigVersion))
	tx.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('updated_at', ?)`,
		time.Now().UTC().Format(time.RFC3339))

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	readerCount := len(sc.Readers)
	sensorCount := 0
	for _, rd := range sc.Readers {
		sensorCount += len(rd.Sensors)
	}
	log.Printf("[sqlite] wrote %d readers, %d sensors, %d coreswitch settings, %d telemetry settings",
		readerCount, sensorCount, len(sc.CoreSwitchSettings), len(sc.TelemetrySettings))
	return nil
}
