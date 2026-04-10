// Package transfer orchestrates the ETL loop: read from InfluxDB, map sensor IDs,
// and send to the Enterprise TP-API.
//
// Sensor mapping source:
//   - SQLite telemetry_settings table when SQLITE_PATH is set (primary — used on all Qubes)
//   - sensor_map.json file when SQLITE_PATH is not set (legacy JSON fallback only)
//
// The sensor map is reloaded on every run so it picks up updates without a restart.
package transfer

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Sandun-S/qube-enterprise-home/enterprise-influx-to-sql/configs"
	"github.com/Sandun-S/qube-enterprise-home/enterprise-influx-to-sql/influxdb"
	"github.com/Sandun-S/qube-enterprise-home/enterprise-influx-to-sql/schema"
	"github.com/Sandun-S/qube-enterprise-home/enterprise-influx-to-sql/sqlite"
	"github.com/Sandun-S/qube-enterprise-home/enterprise-influx-to-sql/tpapi"
)

const batchSize = 1000

// Run performs one complete transfer cycle: load sensor_map, query InfluxDB tables,
// map records to sensor IDs, and POST to TP-API in batches.
func Run(influxClient *influxdb.Client, tpapiClient *tpapi.Client, cfg configs.Config) {
	log.Println("[transfer] ─────────────────────")

	sensorMap := loadSensorMap(cfg)
	if len(sensorMap) == 0 {
		log.Println("[transfer] sensor_map empty or missing — skipping")
		return
	}
	log.Printf("[transfer] sensor_map loaded: %d entries", len(sensorMap))

	tables := cfg.InfluxDB.Tables
	if len(tables) == 0 {
		tables = []string{"Measurements"} // default — matches core-switch DataIn.Table
	}

	end := time.Now().UTC()
	start := end.Add(-time.Duration(cfg.Service.LookbackMins) * time.Minute)

	var allReadings []schema.Reading

	for _, table := range tables {
		recs, err := influxClient.QueryTable(table, start, end)
		if err != nil {
			log.Printf("[influx] query %s failed: %v", table, err)
			continue
		}
		log.Printf("[influx] table %s: %d raw records", table, len(recs))

		for _, rec := range recs {
			sensorID := lookupSensor(sensorMap, rec.Equipment, rec.Reading)
			if sensorID == "" {
				log.Printf("[transfer] no sensor_id for %q.%q — skipping", rec.Equipment, rec.Reading)
				continue
			}
			allReadings = append(allReadings, schema.Reading{
				Time:     rec.Time,
				SensorID: sensorID,
				FieldKey: rec.Reading,
				Value:    rec.Value,
				Unit:     "",
			})
		}
	}

	if len(allReadings) == 0 {
		log.Println("[transfer] no readings to send")
		return
	}

	log.Printf("[transfer] sending %d readings to TP-API", len(allReadings))
	sent, failed := 0, 0
	for i := 0; i < len(allReadings); i += batchSize {
		batchEnd := i + batchSize
		if batchEnd > len(allReadings) {
			batchEnd = len(allReadings)
		}
		if err := tpapiClient.PostReadings(allReadings[i:batchEnd]); err != nil {
			log.Printf("[tpapi] batch failed: %v", err)
			failed += batchEnd - i
		} else {
			sent += batchEnd - i
		}
	}
	log.Printf("[transfer] done — sent=%d failed=%d", sent, failed)
}

// loadSensorMap returns the sensor map for this run.
// When SQLITE_PATH is set, SQLite is the sole source of truth — no JSON fallback.
// Falling back to JSON when SQLite is configured but empty would use stale/wrong mappings.
func loadSensorMap(cfg configs.Config) schema.SensorMap {
	if cfg.Service.SQLitePath != "" {
		m, err := loadFromSQLite(cfg.Service.SQLitePath)
		if err != nil {
			log.Printf("[sensor_map] SQLite load failed: %v", err)
			return nil
		}
		if len(m) == 0 {
			log.Printf("[sensor_map] SQLite telemetry_settings has 0 rows — no mappings configured yet")
		}
		return m
	}
	return loadFromJSON(cfg.Service.SensorMapPath)
}

// loadFromSQLite reads the telemetry_settings table and returns a SensorMap
// with keys normalised to "device.reading" (dot separator).
// Keys stored in SQLite are "device:reading" (colon); converted here for consistency.
func loadFromSQLite(dbPath string) (schema.SensorMap, error) {
	db, err := sqlite.OpenReadOnly(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	raw, err := sqlite.LoadTelemetrySettings(db)
	if err != nil {
		return nil, err
	}

	m := make(schema.SensorMap, len(raw))
	for key, mapping := range raw {
		// Normalise separator: "device:reading" → "device.reading"
		normalised := strings.ReplaceAll(key, ":", ".")
		m[normalised] = mapping.SensorID
	}
	return m, nil
}

// loadFromJSON reads sensor_map.json — format: {"Equipment.Reading": "sensor-uuid"}.
// This file is written by conf-agent when it syncs config from TP-API.
func loadFromJSON(path string) schema.SensorMap {
	if path == "" {
		path = "/config/sensor_map.json"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[sensor_map] read %s: %v", path, err)
		return nil
	}
	var m schema.SensorMap
	if err := json.Unmarshal(data, &m); err != nil {
		log.Printf("[sensor_map] parse error: %v", err)
		return nil
	}
	return m
}

// lookupSensor tries "Equipment.Reading" first, then just "Equipment" as a fallback
// for sensors where every reading maps to the same sensor ID.
func lookupSensor(m schema.SensorMap, equipment, reading string) string {
	if id, ok := m[equipment+"."+reading]; ok {
		return id
	}
	if id, ok := m[equipment]; ok {
		return id
	}
	return ""
}
