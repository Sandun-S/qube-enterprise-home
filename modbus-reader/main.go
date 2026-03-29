// modbus-reader — Qube Enterprise Modbus TCP Reader (v2)
//
// Reads config from shared SQLite → polls Modbus registers → POSTs to coreswitch.
//
// Config changes are handled by conf-agent stopping this container; Swarm recreates
// it and the new instance reads fresh config from SQLite on startup.
//
// Env vars:
//   READER_ID      — UUID of this reader in SQLite
//   SQLITE_PATH    — Path to shared SQLite database (read-only) [default: /opt/qube/data/qube.db]
//   CORESWITCH_URL — Core-switch HTTP endpoint [default: http://core-switch:8585]
//   LOG_LEVEL      — debug, info, warn, error (default: info)
package main

import (
	"fmt"
	"os"
	"sync"

	"github.com/qube-enterprise/pkg/coreswitch"
	"github.com/qube-enterprise/pkg/logger"
	"github.com/qube-enterprise/pkg/sqliteconfig"
	modbuspkg "github.com/qube-enterprise/modbus-reader/modbus"
)

func main() {
	log := logger.New("modbus-reader")

	readerID := os.Getenv("READER_ID")
	if readerID == "" {
		log.Fatal("READER_ID env var not set")
	}

	sqlitePath := os.Getenv("SQLITE_PATH")
	if sqlitePath == "" {
		sqlitePath = "/opt/qube/data/qube.db"
	}

	csURL := os.Getenv("CORESWITCH_URL")
	if csURL == "" {
		csURL = "http://core-switch:8585"
	}

	log.Infof("Starting modbus-reader: reader_id=%s sqlite=%s coreswitch=%s", readerID, sqlitePath, csURL)

	// Open SQLite (read-only)
	db, err := sqliteconfig.OpenReadOnly(sqlitePath)
	if err != nil {
		log.Fatalf("Cannot open SQLite at %s: %v", sqlitePath, err)
	}
	defer db.Close()

	// Load reader config + sensors
	readerCfg, sensors, err := sqliteconfig.LoadReaderConfig(db, readerID)
	if err != nil {
		log.Fatalf("Cannot load reader config for %s: %v", readerID, err)
	}

	log.Infof("Loaded reader '%s' (%s) with %d sensors", readerCfg.Name, readerCfg.Protocol, len(sensors))

	// Parse Modbus connection config
	host := getStr(readerCfg.Config, "host", "localhost")
	port := getInt(readerCfg.Config, "port", 502)
	pollInterval := getInt(readerCfg.Config, "poll_interval_sec", 5)
	slaveID := getInt(readerCfg.Config, "slave_id", 1)
	singleReadCount := getInt(readerCfg.Config, "single_read_count", 100)

	mbCfg := &modbuspkg.ModbusConfig{
		Server:          fmt.Sprintf("modbus-tcp://%s:%d", host, port),
		FreqSec:         pollInterval,
		SlaveID:         slaveID,
		SingleReadCount: singleReadCount,
	}

	// Parse sensors → register readings
	readings := parseSensors(sensors)
	log.Infof("Parsed %d register readings from %d sensors", len(readings), len(sensors))

	if len(readings) == 0 {
		log.Warn("No readings configured — reader will poll but send nothing")
	}

	// Create core-switch client
	csClient := coreswitch.NewClient(csURL, "modbus-reader")

	// Start modbus engine (runs timer loop internally)
	modbuspkg.Init(log, readings, mbCfg, csClient)

	// Block forever
	var wg sync.WaitGroup
	wg.Add(1)
	wg.Wait()
}

// parseSensors converts SQLite sensor configs to modbus Reading structs.
func parseSensors(sensors []sqliteconfig.SensorConfig) []*modbuspkg.Reading {
	var readings []*modbuspkg.Reading

	for _, s := range sensors {
		regs, ok := s.Config["registers"].([]any)
		if !ok {
			continue
		}

		tagStr := sqliteconfig.FormatTags(s.Tags)

		for _, r := range regs {
			rm, ok := r.(map[string]any)
			if !ok {
				continue
			}

			rec := &modbuspkg.Reading{
				Equipment: s.Name,
				Table:     s.Table,
				Output:    s.Output,
				Tags:      tagStr,
			}

			if addr, ok := rm["address"].(float64); ok {
				rec.Addr = uint16(addr)
			}

			rec.RegType = getStr(rm, "register_type", "Holding")
			switch rec.RegType {
			case "holding":
				rec.RegType = "Holding"
			case "input":
				rec.RegType = "Input"
			}

			rec.Reading = getStr(rm, "field_key", "value")
			rec.DataType = getStr(rm, "data_type", "uint16")

			if scale, ok := rm["scale"].(float64); ok {
				rec.Scale = scale
			} else {
				rec.Scale = 1.0
			}

			readings = append(readings, rec)
		}
	}

	return readings
}

func getStr(m map[string]any, key, fallback string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return fallback
}

func getInt(m map[string]any, key string, fallback int) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return fallback
}
