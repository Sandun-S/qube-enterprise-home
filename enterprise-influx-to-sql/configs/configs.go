// Package configs handles loading and validating enterprise-influx-to-sql configuration.
// Config is loaded from a YAML file (configs.yml by default) with environment variable
// overrides applied on top. ${ENV_VAR} placeholders in the YAML are also substituted.
package configs

import (
	"log"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
)

// Config is the top-level configuration structure.
type Config struct {
	Service  ServiceConfig `yaml:"Service"`
	InfluxDB InfluxConfig  `yaml:"InfluxDB"`
	TPAPI    TPAPIConfig   `yaml:"TPAPI"`
}

// ServiceConfig controls the transfer loop behaviour.
type ServiceConfig struct {
	PollInterval  int    `yaml:"PollInterval"`  // seconds between each transfer run
	LookbackMins  int    `yaml:"LookbackMins"`  // query InfluxDB for last N minutes each run
	SensorMapPath string `yaml:"SensorMapPath"` // path to sensor_map.json (JSON fallback)
	SQLitePath    string `yaml:"SQLitePath"`    // optional: path to edge SQLite DB (preferred over JSON)
	Site          string `yaml:"Site"`          // site identifier tag (informational)
}

// InfluxConfig is the InfluxDB v1 connection settings.
type InfluxConfig struct {
	URL    string   `yaml:"URL"`
	DB     string   `yaml:"DB"`
	User   string   `yaml:"User"`
	Pass   string   `yaml:"Pass"`
	Tables []string `yaml:"Tables"` // InfluxDB measurements to query (matches core-switch Table field)
}

// TPAPIConfig is the Enterprise TP-API connection settings.
type TPAPIConfig struct {
	URL       string `yaml:"URL"`
	QubeID    string `yaml:"QubeID"`
	QubeToken string `yaml:"QubeToken"`
}

// Load reads configuration from path, substitutes ${ENV} placeholders, applies env
// overrides, and fills in defaults. Exits on parse error.
func Load(path string) Config {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[config] %s not found — using defaults", path)
		data = defaultYAML()
	}

	// Substitute ${ENV_VAR} placeholders in the YAML text
	str := string(data)
	for _, pair := range os.Environ() {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			str = strings.ReplaceAll(str, "${"+kv[0]+"}", kv[1])
		}
	}

	var cfg Config
	if err := yaml.Unmarshal([]byte(str), &cfg); err != nil {
		log.Fatalf("[config] parse error: %v", err)
	}

	applyEnvOverrides(&cfg)
	applyDefaults(&cfg)

	return cfg
}

// applyEnvOverrides replaces config fields when matching env vars are present.
// Explicit env vars always win over YAML values.
func applyEnvOverrides(cfg *Config) {
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
	if v := os.Getenv("INFLUX_USER"); v != "" {
		cfg.InfluxDB.User = v
	}
	if v := os.Getenv("INFLUX_PASS"); v != "" {
		cfg.InfluxDB.Pass = v
	}
	if v := os.Getenv("SENSOR_MAP_PATH"); v != "" {
		cfg.Service.SensorMapPath = v
	}
	if v := os.Getenv("SQLITE_PATH"); v != "" {
		cfg.Service.SQLitePath = v
	}
	if v := os.Getenv("POLL_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Service.PollInterval = n
		}
	}
	if v := os.Getenv("LOOKBACK_MINS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Service.LookbackMins = n
		}
	}
}

// applyDefaults fills in zero values with production-safe defaults.
func applyDefaults(cfg *Config) {
	if cfg.Service.PollInterval == 0 {
		cfg.Service.PollInterval = 60
	}
	if cfg.Service.LookbackMins == 0 {
		cfg.Service.LookbackMins = 5
	}
	if cfg.Service.SensorMapPath == "" {
		cfg.Service.SensorMapPath = "/config/sensor_map.json"
	}
	if cfg.InfluxDB.URL == "" {
		cfg.InfluxDB.URL = "http://influxdb:8086"
	}
	if cfg.InfluxDB.DB == "" {
		cfg.InfluxDB.DB = "edgex"
	}
	if cfg.TPAPI.URL == "" {
		cfg.TPAPI.URL = "http://cloud-api:8081"
	}
}

// defaultYAML returns a minimal valid YAML config used when no file is found.
func defaultYAML() []byte {
	return []byte(`Service:
  PollInterval: 60
  LookbackMins: 5
InfluxDB:
  URL: "http://influxdb:8086"
  DB: "edgex"
  Tables:
    - "Measurements"
TPAPI:
  URL: "http://cloud-api:8081"
  QubeID: ""
  QubeToken: ""
`)
}
