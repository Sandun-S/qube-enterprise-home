// enterprise-influx-to-sql
// Reads sensor data from InfluxDB v1 (written by the real coreswitch using line protocol),
// maps Equipment+Reading → sensor_id using sensor_map.json (delivered by conf-agent sync),
// and POSTs batches to the Enterprise TP-API /v1/telemetry/ingest endpoint.
//
// This is a DROP-IN replacement for the existing influx-to-sql on Enterprise Qubes.
// It keeps the same InfluxDB v1 connection pattern but sends to Postgres via TP-API
// instead of writing directly to a SQL DB.
//
// config: configs.yml (see configs.yml.example)
// sensor_map: /config/sensor_map.json (injected by conf-agent from TP-API sync)
//
// Uses stdlib only + influxdb1-client.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	client "github.com/influxdata/influxdb1-client/v2"
	"gopkg.in/yaml.v2"
)

// ─── Config ───────────────────────────────────────────────────────────────────

type Config struct {
	Service ServiceConfig `yaml:"Service"`
	InfluxDB InfluxConfig `yaml:"InfluxDB"`
	TPAPI   TPAPIConfig  `yaml:"TPAPI"`
}

type ServiceConfig struct {
	PollInterval int    `yaml:"PollInterval"` // seconds between runs
	LookbackMins int    `yaml:"LookbackMins"` // how far back to query on each run
	SensorMapPath string `yaml:"SensorMapPath"` // path to sensor_map.json
	Site          string `yaml:"Site"`
}

type InfluxConfig struct {
	URL      string `yaml:"URL"`
	DB       string `yaml:"DB"`
	User     string `yaml:"User"`
	Pass     string `yaml:"Pass"`
	// Tables to query — matches the "Table" field from coreswitch DataIn
	// e.g. ["Measurements"] — leave empty to query all
	Tables []string `yaml:"Tables"`
}

type TPAPIConfig struct {
	URL       string `yaml:"URL"`
	QubeID    string `yaml:"QubeID"`
	QubeToken string `yaml:"QubeToken"`
}

// ─── Telemetry Reading ────────────────────────────────────────────────────────

type Reading struct {
	Time     time.Time `json:"time"`
	SensorID string    `json:"sensor_id"`
	FieldKey string    `json:"field_key"`
	Value    float64   `json:"value"`
	Unit     string    `json:"unit"`
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	cfgPath := getenv("CONFIG_PATH", "configs.yml")
	cfg := loadConfig(cfgPath)

	log.Printf("[enterprise-influx-to-sql] starting — influx=%s tpapi=%s qube=%s interval=%ds",
		cfg.InfluxDB.URL, cfg.TPAPI.URL, cfg.TPAPI.QubeID, cfg.Service.PollInterval)

	// Verify InfluxDB connection
	for {
		if err := pingInflux(cfg.InfluxDB); err != nil {
			log.Printf("[influx] not reachable: %v — retrying in 10s", err)
			time.Sleep(10 * time.Second)
		} else {
			log.Println("[influx] connected")
			break
		}
	}

	ticker := time.NewTicker(time.Duration(cfg.Service.PollInterval) * time.Second)
	defer ticker.Stop()

	// run once immediately
	runTransfer(cfg)
	for range ticker.C {
		runTransfer(cfg)
	}
}

// ─── Transfer ─────────────────────────────────────────────────────────────────

func runTransfer(cfg Config) {
	log.Println("[transfer] ─────────────────────")

	// Load sensor_map.json (re-read on each cycle so it picks up updates)
	sensorMap := loadSensorMap(cfg.Service.SensorMapPath)
	if len(sensorMap) == 0 {
		log.Println("[transfer] sensor_map.json empty or missing — skipping")
		return
	}
	log.Printf("[transfer] sensor_map loaded: %d entries", len(sensorMap))

	tables := cfg.InfluxDB.Tables
	if len(tables) == 0 {
		tables = []string{"Measurements"} // default — matches coreswitch default Table
	}

	allReadings := make([]Reading, 0, 500)
	end := time.Now().UTC()
	start := end.Add(-time.Duration(cfg.Service.LookbackMins) * time.Minute)

	for _, table := range tables {
		recs, err := queryInflux(cfg.InfluxDB, table, start, end)
		if err != nil {
			log.Printf("[influx] query %s failed: %v", table, err)
			continue
		}
		log.Printf("[influx] table %s: %d raw records", table, len(recs))

		for _, rec := range recs {
			// sensor_map key format: "Equipment.Reading"
			// This matches how coreswitch writes tags: device=Equipment, reading=Reading
			key := rec.Equipment + "." + rec.Reading
			sensorID, ok := sensorMap[key]
			if !ok {
				// also try just Equipment (for simple sensors with one reading)
				sensorID, ok = sensorMap[rec.Equipment]
				if !ok {
					log.Printf("[transfer] no sensor_id for key %q — skipping", key)
					continue
				}
			}
			allReadings = append(allReadings, Reading{
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

	// Send in batches of 1000
	batchSize := 1000
	sent := 0
	failed := 0
	for i := 0; i < len(allReadings); i += batchSize {
		end := i + batchSize
		if end > len(allReadings) {
			end = len(allReadings)
		}
		batch := allReadings[i:end]
		if err := postReadings(cfg.TPAPI, batch); err != nil {
			log.Printf("[tpapi] batch send failed: %v", err)
			failed += len(batch)
		} else {
			sent += len(batch)
		}
	}
	log.Printf("[transfer] done — sent=%d failed=%d", sent, failed)
}

// ─── InfluxDB v1 Query ────────────────────────────────────────────────────────

type rawRecord struct {
	Time      time.Time
	Equipment string
	Reading   string
	Value     float64
}

func pingInflux(cfg InfluxConfig) error {
	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     cfg.URL,
		Username: cfg.User,
		Password: cfg.Pass,
	})
	if err != nil {
		return err
	}
	defer c.Close()
	_, _, err = c.Ping(5 * time.Second)
	return err
}

// queryInflux queries an InfluxDB v1 measurement (Table) for all readings
// in the time range. The coreswitch writes with tags: device=Equipment, reading=Reading.
func queryInflux(cfg InfluxConfig, table string, start, end time.Time) ([]rawRecord, error) {
	layout := "2006-01-02 15:04:05"
	q := fmt.Sprintf(
		`SELECT mean(value) FROM "%s" WHERE time >= '%s' AND time < '%s' GROUP BY time(1m), device, reading ORDER BY time ASC`,
		table,
		start.Format(layout),
		end.Format(layout),
	)

	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     cfg.URL,
		Username: cfg.User,
		Password: cfg.Pass,
	})
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}
	defer c.Close()

	resp, err := c.Query(client.Query{Command: q, Database: cfg.DB})
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	if resp.Error() != nil {
		return nil, fmt.Errorf("response: %w", resp.Error())
	}

	var recs []rawRecord
	for _, r := range resp.Results {
		for _, sr := range r.Series {
			device := sr.Tags["device"]
			reading := sr.Tags["reading"]
			for _, vals := range sr.Values {
				if len(vals) < 2 || vals[1] == nil {
					continue
				}
				t, _ := time.Parse("2006-01-02T15:04:05Z", fmt.Sprintf("%v", vals[0]))
				v, _ := strconv.ParseFloat(fmt.Sprintf("%v", vals[1]), 64)
				recs = append(recs, rawRecord{
					Time: t, Equipment: device, Reading: reading, Value: v,
				})
			}
		}
	}
	return recs, nil
}

// ─── TP-API Client ────────────────────────────────────────────────────────────

func postReadings(cfg TPAPIConfig, readings []Reading) error {
	body, err := json.Marshal(map[string]any{"readings": readings})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest("POST", cfg.URL+"/v1/telemetry/ingest", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Qube-ID", cfg.QubeID)
	req.Header.Set("Authorization", "Bearer "+cfg.QubeToken)

	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("tp-api returned %d: %s", resp.StatusCode, b)
	}

	var result map[string]any
	json.Unmarshal(b, &result)
	log.Printf("[tpapi] ingest result: inserted=%v failed=%v", result["inserted"], result["failed"])
	return nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// loadSensorMap reads sensor_map.json — format: {"Equipment.Reading": "sensor-uuid"}
// This file is written by conf-agent when it syncs config from TP-API.
func loadSensorMap(path string) map[string]string {
	if path == "" {
		path = "/config/sensor_map.json"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[sensor_map] read error: %v", err)
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		log.Printf("[sensor_map] parse error: %v", err)
		return nil
	}
	return m
}

func loadConfig(path string) Config {
	data, err := os.ReadFile(path)
	if err != nil {
		// Config file missing — use defaults + env vars only
		log.Printf("[config] %s not found, using environment variables only", path)
		data = []byte(`Service:
  PollInterval: 60
  LookbackMins: 5
InfluxDB:
  URL: "http://influxdb:8086"
  DB: "edgex"
TPAPI:
  URL: "http://cloud-api:8081"
  QubeID: "Q-1001"
  QubeToken: ""
`)
	}

	// Override from env
	str := string(data)
	for _, pair := range os.Environ() {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			str = strings.ReplaceAll(str, "${"+kv[0]+"}", kv[1])
		}
	}

	var cfg Config
	if err := yaml.Unmarshal([]byte(str), &cfg); err != nil {
		log.Fatalf("config parse error: %v", err)
	}

	// Env overrides (explicit)
	if v := os.Getenv("TPAPI_URL"); v != "" {
		cfg.TPAPI.URL = v
	}
	if v := os.Getenv("QUBE_ID"); v != "" {
		cfg.TPAPI.QubeID = v
	}
	if v := os.Getenv("QUBE_TOKEN"); v != "" {
		cfg.TPAPI.QubeToken = v
	}
	if v := os.Getenv("INFLUX_URL"); v != "" {
		cfg.InfluxDB.URL = v
	}
	if v := os.Getenv("INFLUX_DB"); v != "" {
		cfg.InfluxDB.DB = v
	}
	if v := os.Getenv("SENSOR_MAP_PATH"); v != "" {
		cfg.Service.SensorMapPath = v
	}

	// Defaults
	if cfg.Service.PollInterval == 0 {
		cfg.Service.PollInterval = 60
	}
	if cfg.Service.LookbackMins == 0 {
		cfg.Service.LookbackMins = 5
	}
	if cfg.Service.SensorMapPath == "" {
		cfg.Service.SensorMapPath = "/config/sensor_map.json"
	}

	return cfg
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
