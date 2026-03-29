// Package configs handles conf-agent startup configuration.
// Reads from environment variables and /boot/mit.txt (device identity file).
package configs

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for conf-agent.
type Config struct {
	TPAPIURL     string        // HTTP polling fallback (port 8081)
	CloudWSURL   string        // WebSocket primary (port 8080)
	QubeID       string
	QubeToken    string
	RegisterKey  string        // from /boot/mit.txt — used for self-registration
	WorkDir      string
	SQLitePath   string        // /opt/qube/data/qube.db
	PollInterval time.Duration
	MitTxtPath   string
}

// MitTxt holds the device identity written at flash time by image-install.sh.
type MitTxt struct {
	DeviceID    string
	DeviceName  string
	DeviceType  string
	RegisterKey string
	MaintainKey string
}

// LoadConfig reads all config from environment variables.
func LoadConfig() Config {
	interval, _ := strconv.Atoi(Getenv("POLL_INTERVAL", "30"))
	return Config{
		TPAPIURL:     Getenv("TPAPI_URL", "http://localhost:8081"),
		CloudWSURL:   Getenv("CLOUD_WS_URL", "ws://localhost:8080/ws"),
		QubeID:       Getenv("QUBE_ID", ""),
		QubeToken:    Getenv("QUBE_TOKEN", ""),
		RegisterKey:  Getenv("REGISTER_KEY", ""),
		WorkDir:      Getenv("WORK_DIR", "/opt/qube"),
		SQLitePath:   Getenv("SQLITE_PATH", "/opt/qube/data/qube.db"),
		MitTxtPath:   Getenv("MIT_TXT_PATH", "/boot/mit.txt"),
		PollInterval: time.Duration(interval) * time.Second,
	}
}

// ReadMitTxt parses the device identity file written at flash time.
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

// Getenv returns the env var value or a fallback.
func Getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
