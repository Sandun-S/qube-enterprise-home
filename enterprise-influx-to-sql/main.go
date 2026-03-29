// enterprise-influx-to-sql v2
//
// Reads sensor data from InfluxDB v1 (written by core-switch via line protocol),
// maps Equipment+Reading → sensor_id using sensor_map from SQLite or a JSON file,
// and POSTs batches to the Enterprise TP-API /v1/telemetry/ingest endpoint.
//
// This is a DROP-IN replacement for the v1 influx-to-sql on Enterprise Qubes.
// It keeps the same InfluxDB v1 query pattern but sends to Postgres via TP-API
// instead of writing directly to a SQL database.
//
// Sensor map priority:
//  1. SQLite telemetry_settings table (when SQLITE_PATH is set)
//  2. sensor_map.json file (SensorMapPath / SENSOR_MAP_PATH env)
//
// Key environment variables:
//
//	CONFIG_PATH      — config file path [default: configs.yml]
//	SQLITE_PATH      — edge SQLite DB path (preferred sensor map source)
//	TPAPI_URL        — override TPAPI.URL from config
//	QUBE_ID          — override TPAPI.QubeID from config
//	QUBE_TOKEN       — override TPAPI.QubeToken from config
//	INFLUX_URL       — override InfluxDB.URL from config
//	INFLUX_DB        — override InfluxDB.DB from config
//	SENSOR_MAP_PATH  — override Service.SensorMapPath from config
//	POLL_INTERVAL    — override Service.PollInterval (seconds)
//	LOOKBACK_MINS    — override Service.LookbackMins
package main

import (
	"log"
	"os"
	"time"

	"github.com/Sandun-S/qube-enterprise-home/enterprise-influx-to-sql/configs"
	"github.com/Sandun-S/qube-enterprise-home/enterprise-influx-to-sql/influxdb"
	"github.com/Sandun-S/qube-enterprise-home/enterprise-influx-to-sql/tpapi"
	"github.com/Sandun-S/qube-enterprise-home/enterprise-influx-to-sql/transfer"
)

func main() {
	cfgPath := getenv("CONFIG_PATH", "configs.yml")
	cfg := configs.Load(cfgPath)

	log.Printf("[enterprise-influx-to-sql] starting — influx=%s tpapi=%s qube=%s interval=%ds lookback=%dm",
		cfg.InfluxDB.URL, cfg.TPAPI.URL, cfg.TPAPI.QubeID, cfg.Service.PollInterval, cfg.Service.LookbackMins)

	influxClient := influxdb.New(cfg.InfluxDB)

	// Wait until InfluxDB is reachable before starting the transfer loop
	for {
		if err := influxClient.Ping(); err != nil {
			log.Printf("[influx] not reachable: %v — retrying in 10s", err)
			time.Sleep(10 * time.Second)
		} else {
			log.Println("[influx] connected")
			break
		}
	}

	tpapiClient := tpapi.New(cfg.TPAPI)

	ticker := time.NewTicker(time.Duration(cfg.Service.PollInterval) * time.Second)
	defer ticker.Stop()

	// Run once immediately, then on each ticker tick
	transfer.Run(influxClient, tpapiClient, cfg)
	for range ticker.C {
		transfer.Run(influxClient, tpapiClient, cfg)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
