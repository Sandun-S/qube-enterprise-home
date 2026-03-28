// snmp-reader — Qube Enterprise SNMP Reader (v2)
//
// Reads config from shared SQLite → polls SNMP OIDs from multiple devices →
// POSTs to coreswitch.
//
// SNMP is a "multi_target" reader: one reader container polls multiple devices,
// each device defined as a sensor with its own IP/community in config_json.
//
// Env vars:
//   READER_ID      — UUID of this reader in SQLite
//   SQLITE_PATH    — Path to shared SQLite database (read-only)
//   CORESWITCH_URL — Core-switch HTTP endpoint
//   LOG_LEVEL      — debug, info, warn, error (default: info)
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/qube-enterprise/pkg/coreswitch"
	"github.com/qube-enterprise/pkg/logger"
	"github.com/qube-enterprise/pkg/sqliteconfig"
)

// snmpTarget represents one device to poll.
type snmpTarget struct {
	sensor    sqliteconfig.SensorConfig
	host      string
	port      int
	community string
	version   gosnmp.SnmpVersion
	oids      []oidMapping
}

// oidMapping maps one OID to a field key.
type oidMapping struct {
	oid      string
	fieldKey string
	scale    float64
}

func main() {
	log := logger.New("snmp-reader")

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
	timeout := getInt(readerCfg.Config, "timeout_ms", 5000)
	retries := getInt(readerCfg.Config, "retries", 2)

	// ── Build SNMP targets from sensors ──────────────────────────────────
	// Each sensor = one device with its own IP/community/OIDs
	var targets []snmpTarget
	for _, s := range sensors {
		host := getString(s.Config, "host", "")
		if host == "" {
			log.Warnf("Sensor %s has no host — skipping", s.Name)
			continue
		}

		port := getInt(s.Config, "port", 161)
		community := getString(s.Config, "community", "public")
		versionStr := getString(s.Config, "version", "2c")

		var version gosnmp.SnmpVersion
		switch versionStr {
		case "1":
			version = gosnmp.Version1
		case "3":
			version = gosnmp.Version3
		default:
			version = gosnmp.Version2c
		}

		// Parse OID mappings
		oidList, ok := s.Config["oids"].([]any)
		if !ok {
			log.Warnf("Sensor %s has no oids array — skipping", s.Name)
			continue
		}

		var oids []oidMapping
		for _, o := range oidList {
			om, ok := o.(map[string]any)
			if !ok {
				continue
			}
			oid := getString(om, "oid", "")
			if oid == "" {
				continue
			}
			oids = append(oids, oidMapping{
				oid:      oid,
				fieldKey: getString(om, "field_key", oid),
				scale:    getFloat(om, "scale", 1.0),
			})
		}

		if len(oids) == 0 {
			continue
		}

		targets = append(targets, snmpTarget{
			sensor:    s,
			host:      host,
			port:      port,
			community: community,
			version:   version,
			oids:      oids,
		})
	}

	if len(targets) == 0 {
		log.Warn("No SNMP targets configured — exiting")
		return
	}

	totalOIDs := 0
	for _, t := range targets {
		totalOIDs += len(t.oids)
	}
	log.Infof("Polling %d devices, %d total OIDs every %ds", len(targets), totalOIDs, pollIntervalSec)

	// ── Core-switch client ───────────────────────────────────────────────
	csClient := coreswitch.NewClient(coreSwitchURL, "snmp-reader")

	// ── Poll loop ────────────────────────────────────────────────────────
	ticker := time.NewTicker(time.Duration(pollIntervalSec) * time.Second)
	defer ticker.Stop()

	pollAll(log, csClient, targets, readerCfg.Name, timeout, retries)
	for range ticker.C {
		pollAll(log, csClient, targets, readerCfg.Name, timeout, retries)
	}
}

func pollAll(log interface{ Infof(string, ...any); Warnf(string, ...any); Debugf(string, ...any) },
	csClient *coreswitch.Client, targets []snmpTarget, readerName string,
	timeoutMs, retries int) {

	for _, t := range targets {
		pollTarget(log, csClient, t, readerName, timeoutMs, retries)
	}
}

func pollTarget(log interface{ Warnf(string, ...any); Debugf(string, ...any) },
	csClient *coreswitch.Client, target snmpTarget, readerName string,
	timeoutMs, retries int) {

	client := &gosnmp.GoSNMP{
		Target:    target.host,
		Port:      uint16(target.port),
		Community: target.community,
		Version:   target.version,
		Timeout:   time.Duration(timeoutMs) * time.Millisecond,
		Retries:   retries,
	}

	if err := client.Connect(); err != nil {
		log.Warnf("SNMP connect failed for %s (%s): %v", target.sensor.Name, target.host, err)
		return
	}
	defer client.Conn.Close()

	// Collect all OIDs to poll
	oidStrs := make([]string, len(target.oids))
	for i, o := range target.oids {
		oidStrs[i] = o.oid
	}

	result, err := client.Get(oidStrs)
	if err != nil {
		log.Warnf("SNMP GET failed for %s (%s): %v", target.sensor.Name, target.host, err)
		return
	}

	// Build OID → mapping lookup
	oidMap := map[string]oidMapping{}
	for _, o := range target.oids {
		oidMap[o.oid] = o
	}

	tags := sqliteconfig.FormatTags(target.sensor.Tags)
	var readings []coreswitch.DataIn

	for _, pdu := range result.Variables {
		oid := strings.TrimPrefix(pdu.Name, ".")
		mapping, ok := oidMap[oid]
		if !ok {
			// Try with leading dot
			mapping, ok = oidMap["."+oid]
			if !ok {
				continue
			}
		}

		val := extractSNMPValue(pdu)
		if val == "" {
			continue
		}

		// Apply scale if numeric
		if mapping.scale != 1.0 {
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				val = formatValue(f * mapping.scale)
			}
		}

		readings = append(readings, coreswitch.DataIn{
			Table:     target.sensor.Table,
			Equipment: target.sensor.Name,
			Reading:   mapping.fieldKey,
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
			log.Debugf("Sent %d readings for %s (%s)", len(readings), target.sensor.Name, target.host)
		}
	}
}

func extractSNMPValue(pdu gosnmp.SnmpPDU) string {
	switch pdu.Type {
	case gosnmp.Integer:
		return strconv.Itoa(pdu.Value.(int))
	case gosnmp.Counter32, gosnmp.Gauge32, gosnmp.TimeTicks:
		return strconv.FormatUint(uint64(pdu.Value.(uint)), 10)
	case gosnmp.Counter64:
		return strconv.FormatUint(pdu.Value.(uint64), 10)
	case gosnmp.OctetString:
		return string(pdu.Value.([]byte))
	case gosnmp.ObjectIdentifier:
		return pdu.Value.(string)
	default:
		return fmt.Sprintf("%v", pdu.Value)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

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
