// http-reader — Qube Enterprise HTTP/JSON Reader (v2)
//
// Reads config from shared SQLite → polls HTTP/REST endpoints → extracts values
// via JSON path → POSTs to coreswitch.
//
// HTTP is a "multi_target" reader: one reader container polls multiple endpoints,
// each defined as a sensor with its own URL/auth/JSON paths in config_json.
//
// Env vars:
//   READER_ID      — UUID of this reader in SQLite
//   SQLITE_PATH    — Path to shared SQLite database (read-only)
//   CORESWITCH_URL — Core-switch HTTP endpoint
//   LOG_LEVEL      — debug, info, warn, error (default: info)
package main

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/qube-enterprise/pkg/coreswitch"
	"github.com/qube-enterprise/pkg/logger"
	"github.com/qube-enterprise/pkg/sqliteconfig"
	"github.com/tidwall/gjson"
)

// httpTarget represents one HTTP endpoint to poll.
type httpTarget struct {
	sensor    sqliteconfig.SensorConfig
	url       string
	method    string
	headers   map[string]string
	authType  string // "none", "basic", "bearer"
	authToken string
	username  string
	password  string
	paths     []jsonPathMapping
}

// jsonPathMapping maps one JSON path to a field key.
type jsonPathMapping struct {
	jsonPath string
	fieldKey string
	scale    float64
}

func main() {
	log := logger.New("http-reader")

	readerID := os.Getenv("READER_ID")
	sqlitePath := os.Getenv("SQLITE_PATH")
	coreSwitchURL := getenv("CORESWITCH_URL", "http://core-switch:8585")

	if readerID == "" || sqlitePath == "" {
		log.Fatal("READER_ID and SQLITE_PATH are required")
	}

	// ── Load config from SQLite ──────────────────────────────────────────
	db, err := sqliteconfig.OpenReadOnly(sqlitePath)
	if err != nil {
		log.Fatalf("Failed to open SQLite: %v", err)
	}

	readerCfg, sensors, err := sqliteconfig.LoadReaderConfig(db, readerID)
	db.Close()

	if err != nil {
		log.Fatalf("Failed to load reader config: %v", err)
	}

	log.Infof("Loaded reader: name=%s protocol=%s sensors=%d",
		readerCfg.Name, readerCfg.Protocol, len(sensors))

	if len(sensors) == 0 {
		log.Warn("No sensors configured — exiting")
		return
	}

	// ── Parse poll interval from reader config ───────────────────────────
	pollIntervalSec := getInt(readerCfg.Config, "poll_interval_sec", 30)
	timeout := getInt(readerCfg.Config, "timeout_ms", 10000)

	// ── Build HTTP targets from sensors ──────────────────────────────────
	httpClient := &http.Client{Timeout: time.Duration(timeout) * time.Millisecond}

	var targets []httpTarget
	for _, s := range sensors {
		url := getString(s.Config, "url", "")
		if url == "" {
			log.Warnf("Sensor %s has no url — skipping", s.Name)
			continue
		}

		method := strings.ToUpper(getString(s.Config, "method", "GET"))
		authType := getString(s.Config, "auth_type", "none")

		// Parse headers
		headers := map[string]string{}
		if hdr, ok := s.Config["headers"].(map[string]any); ok {
			for k, v := range hdr {
				if vs, ok := v.(string); ok {
					headers[k] = vs
				}
			}
		}

		// Parse JSON path mappings
		pathList, ok := s.Config["json_paths"].([]any)
		if !ok {
			log.Warnf("Sensor %s has no json_paths array — skipping", s.Name)
			continue
		}

		var paths []jsonPathMapping
		for _, p := range pathList {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			jp := getString(pm, "json_path", "")
			if jp == "" {
				continue
			}
			paths = append(paths, jsonPathMapping{
				jsonPath: normalisePath(jp),
				fieldKey: getString(pm, "field_key", jp),
				scale:    getFloat(pm, "scale", 1.0),
			})
		}

		if len(paths) == 0 {
			continue
		}

		targets = append(targets, httpTarget{
			sensor:    s,
			url:       url,
			method:    method,
			headers:   headers,
			authType:  authType,
			authToken: getString(s.Config, "bearer_token", ""),
			username:  getString(s.Config, "username", ""),
			password:  getString(s.Config, "password", ""),
			paths:     paths,
		})
	}

	if len(targets) == 0 {
		log.Warn("No HTTP targets configured — exiting")
		return
	}

	totalPaths := 0
	for _, t := range targets {
		totalPaths += len(t.paths)
	}
	log.Infof("Polling %d endpoints, %d total JSON paths every %ds",
		len(targets), totalPaths, pollIntervalSec)

	// ── Core-switch client ───────────────────────────────────────────────
	csClient := coreswitch.NewClient(coreSwitchURL, "http-reader")

	// ── Poll loop ────────────────────────────────────────────────────────
	ticker := time.NewTicker(time.Duration(pollIntervalSec) * time.Second)
	defer ticker.Stop()

	pollAll(log, httpClient, csClient, targets, readerCfg.Name)
	for range ticker.C {
		pollAll(log, httpClient, csClient, targets, readerCfg.Name)
	}
}

func pollAll(log interface{ Warnf(string, ...any); Debugf(string, ...any) },
	httpClient *http.Client, csClient *coreswitch.Client,
	targets []httpTarget, readerName string) {

	for _, t := range targets {
		pollEndpoint(log, httpClient, csClient, t, readerName)
	}
}

func pollEndpoint(log interface{ Warnf(string, ...any); Debugf(string, ...any) },
	httpClient *http.Client, csClient *coreswitch.Client,
	target httpTarget, readerName string) {

	req, err := http.NewRequest(target.method, target.url, nil)
	if err != nil {
		log.Warnf("Failed to create request for %s: %v", target.sensor.Name, err)
		return
	}

	// Set headers
	for k, v := range target.headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}

	// Auth
	switch target.authType {
	case "basic":
		req.SetBasicAuth(target.username, target.password)
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+target.authToken)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Warnf("HTTP request failed for %s (%s): %v", target.sensor.Name, target.url, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Warnf("HTTP %d from %s for %s", resp.StatusCode, target.url, target.sensor.Name)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Warnf("Failed to read response from %s: %v", target.url, err)
		return
	}

	jsonStr := string(body)
	if !gjson.Valid(jsonStr) {
		log.Warnf("Invalid JSON response from %s", target.url)
		return
	}

	tags := sqliteconfig.FormatTags(target.sensor.Tags)
	var readings []coreswitch.DataIn

	for _, pm := range target.paths {
		result := gjson.Get(jsonStr, pm.jsonPath)
		if !result.Exists() {
			log.Warnf("JSON path %q not found in response from %s", pm.jsonPath, target.url)
			continue
		}

		val := result.String()
		// Apply scale if numeric
		if pm.scale != 1.0 {
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				val = formatValue(f * pm.scale)
			}
		}

		readings = append(readings, coreswitch.DataIn{
			Table:     target.sensor.Table,
			Equipment: target.sensor.Name,
			Reading:   pm.fieldKey,
			Output:    target.sensor.Output,
			Sender:    readerName,
			Tags:      tags,
			Time:      time.Now().UnixMicro(),
			Value:     val,
		})
	}

	if len(readings) > 0 {
		if err := csClient.SendBatch(readings); err != nil {
			log.Warnf("Failed to send batch for %s: %v", target.sensor.Name, err)
		} else {
			log.Debugf("Sent %d readings from %s", len(readings), target.url)
		}
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func normalisePath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, "$[")
	return path
}

func formatValue(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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

