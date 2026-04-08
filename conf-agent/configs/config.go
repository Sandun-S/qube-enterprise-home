// Package configs handles conf-agent startup configuration.
//
// Config is loaded in two steps:
//  1. LoadConfigs() reads the YAML file (same as v1 conf-agent-master)
//  2. ApplyEnvOverrides() overlays enterprise env vars on top
//
// Device identity (QubeID, RegisterKey, MaintainKey) is read from /boot/mit.txt
// by ReadMitTxt() and merged into Config by main().
package configs

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

// ─── Config struct ────────────────────────────────────────────────────────────

// Config holds all runtime configuration for conf-agent.
// YAML fields match v1 config.yml; enterprise fields are set from env vars.
type Config struct {
	// ── v1 fields (from config.yml) ──────────────────────────────────────────
	LogLevel        string `yaml:"LogLevel"`
	Port            int    `yaml:"Port"`            // local HTTP server port (web UI)
	DownloadTimeout int    `yaml:"DownloadTimeout"` // seconds; used for large file transfers

	// ── Enterprise fields (env vars, overlaid after YAML load) ───────────────
	TPAPIURL     string        // http://<cloud>:8081  — TP-API (HMAC, device-facing)
	CloudWSURL   string        // ws://<cloud>:8080/ws — WebSocket (primary sync)
	QubeID       string        // Device ID (from mit.txt or QUBE_ID env)
	QubeToken    string        // Auth token (auto-obtained via self-registration)
	RegisterKey  string        // Registration key (from mit.txt or REGISTER_KEY env)
	WorkDir      string        // Working directory on device (default /opt/qube)
	SQLitePath   string        // SQLite path (default /opt/qube/data/qube.db)
	PollInterval time.Duration // How often to poll TP-API as fallback
	MitTxtPath   string        // Device identity file (default /boot/mit.txt)
}

// ─── MitTxt ──────────────────────────────────────────────────────────────────

// MitTxt holds the device identity written at flash time by image-install.sh.
// The file is YAML-like with "key: value" lines.
type MitTxt struct {
	DeviceID    string
	DeviceName  string
	DeviceType  string
	RegisterKey string
	MaintainKey string
}

// ─── Loaders ─────────────────────────────────────────────────────────────────

var log *logrus.Logger

// LoadConfigs reads the YAML config file and returns a *Config.
// The config file is optional — if missing, defaults + env vars are used.
// This supports bare-binary deployment (Multipass/systemd) where no config.yml
// is present and all settings come from environment variables.
func LoadConfigs(l *logrus.Logger, confFile string) *Config {
	log = l

	conf := &Config{}

	fd, err := os.Open(confFile)
	if err != nil {
		// Config file is optional in v2: all enterprise settings come from env vars.
		log.Warnf("[config] config file %s not found — using defaults + env vars", confFile)
	} else {
		defer fd.Close()
		decoder := yaml.NewDecoder(fd)
		if err := decoder.Decode(conf); err != nil {
			log.Fatalf("Config file parse error: %s", err)
		}
		// Set log level from YAML
		if conf.LogLevel != "" {
			lvl, err := logrus.ParseLevel(conf.LogLevel)
			if err == nil {
				log.SetLevel(lvl)
			}
		}
	}

	// Apply defaults for fields not set in YAML
	if conf.Port == 0 {
		conf.Port = 8081
	}
	if conf.DownloadTimeout == 0 {
		conf.DownloadTimeout = 7200
	}

	// Overlay enterprise env vars
	ApplyEnvOverrides(conf)

	log.Printf("[config] loaded: port=%d loglevel=%s tpapi=%s ws=%s workdir=%s",
		conf.Port, conf.LogLevel, conf.TPAPIURL, conf.CloudWSURL, conf.WorkDir)

	return conf
}

// ApplyEnvOverrides overlays enterprise environment variables onto conf.
// Called automatically by LoadConfigs; can also be called manually for testing.
func ApplyEnvOverrides(conf *Config) {
	if v := os.Getenv("TPAPI_URL"); v != "" {
		conf.TPAPIURL = v
	}
	if conf.TPAPIURL == "" {
		conf.TPAPIURL = "http://localhost:8081"
	}

	if v := os.Getenv("CLOUD_WS_URL"); v != "" {
		conf.CloudWSURL = v
	}
	if conf.CloudWSURL == "" {
		conf.CloudWSURL = "ws://localhost:8080/ws"
	}

	if v := os.Getenv("QUBE_ID"); v != "" {
		conf.QubeID = v
	}
	if v := os.Getenv("QUBE_TOKEN"); v != "" {
		conf.QubeToken = v
	}
	if v := os.Getenv("REGISTER_KEY"); v != "" {
		conf.RegisterKey = v
	}

	if v := os.Getenv("WORK_DIR"); v != "" {
		conf.WorkDir = v
	}
	if conf.WorkDir == "" {
		conf.WorkDir = "/opt/qube"
	}

	if v := os.Getenv("SQLITE_PATH"); v != "" {
		conf.SQLitePath = v
	}
	if conf.SQLitePath == "" {
		conf.SQLitePath = "/opt/qube/data/qube.db"
	}

	if v := os.Getenv("MIT_TXT_PATH"); v != "" {
		conf.MitTxtPath = v
	}
	if conf.MitTxtPath == "" {
		conf.MitTxtPath = "/boot/mit.txt"
	}

	interval := 30
	if v := os.Getenv("POLL_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			interval = n
		}
	}
	conf.PollInterval = time.Duration(interval) * time.Second
}

// ReadMitTxt parses the device identity file written at flash time.
// The file uses "key: value" lines (YAML-like, but hand-parsed).
func ReadMitTxt(path string) (*MitTxt, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := &MitTxt{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "deviceid":
			m.DeviceID = val
		case "devicename":
			m.DeviceName = val
		case "devicetype":
			m.DeviceType = val
		case "register":
			m.RegisterKey = val
		case "maintain":
			m.MaintainKey = val
		}
	}

	if m.DeviceID == "" {
		return nil, fmt.Errorf("deviceid not found in %s", path)
	}
	return m, nil
}
