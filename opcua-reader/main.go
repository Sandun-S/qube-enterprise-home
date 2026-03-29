// opcua-reader — Qube Enterprise OPC-UA Reader (v2)
//
// Reads config from shared SQLite → connects to OPC-UA server → reads node values
// → POSTs to coreswitch.
//
// Supports both polling and subscription modes (configured per sensor).
//
// Env vars:
//   READER_ID      — UUID of this reader in SQLite
//   SQLITE_PATH    — Path to shared SQLite database (read-only)
//   CORESWITCH_URL — Core-switch HTTP endpoint
//   LOG_LEVEL      — debug, info, warn, error (default: info)
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
	"github.com/Sandun-S/qube-enterprise-home/opcua-reader/coreswitch"
	"github.com/Sandun-S/qube-enterprise-home/opcua-reader/logger"
	"github.com/Sandun-S/qube-enterprise-home/opcua-reader/configs"
)

// sensorNodeGroup groups a sensor with its OPC-UA node mappings.
type sensorNodeGroup struct {
	sensor configs.SensorConfig
	nodes  []nodeMapping
}

// nodeMapping maps one OPC-UA node to a field key.
type nodeMapping struct {
	nodeID   string
	fieldKey string
	scale    float64
}

func main() {
	log := logger.New("opcua-reader")

	readerID := os.Getenv("READER_ID")
	sqlitePath := os.Getenv("SQLITE_PATH")
	coreSwitchURL := getenv("CORESWITCH_URL", "http://core-switch:8585")

	if readerID == "" || sqlitePath == "" {
		log.Fatal("READER_ID and SQLITE_PATH are required")
	}

	// ── Load config from SQLite ──────────────────────────────────────────
	db, err := configs.OpenReadOnly(sqlitePath)
	if err != nil {
		log.Fatalf("Failed to open SQLite: %v", err)
	}

	readerCfg, sensors, err := configs.LoadReaderConfig(db, readerID)
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

	// ── Parse reader connection config ───────────────────────────────────
	endpoint := getString(readerCfg.Config, "endpoint", "opc.tcp://localhost:4840")
	pollIntervalSec := getInt(readerCfg.Config, "poll_interval_sec", 5)
	securityPolicy := getString(readerCfg.Config, "security_policy", "None")
	securityMode := getString(readerCfg.Config, "security_mode", "None")

	log.Infof("OPC-UA endpoint: %s (poll=%ds, security=%s/%s)",
		endpoint, pollIntervalSec, securityPolicy, securityMode)

	// ── Build node mappings from sensors ──────────────────────────────────
	var sensorList []sensorNodeGroup

	for _, s := range sensors {
		nodeList, ok := s.Config["nodes"].([]any)
		if !ok {
			log.Warnf("Sensor %s has no nodes array — skipping", s.Name)
			continue
		}

		var nodes []nodeMapping
		for _, n := range nodeList {
			nm, ok := n.(map[string]any)
			if !ok {
				continue
			}
			nodeID := getString(nm, "node_id", "")
			if nodeID == "" {
				continue
			}
			nodes = append(nodes, nodeMapping{
				nodeID:   nodeID,
				fieldKey: getString(nm, "field_key", nodeID),
				scale:    getFloat(nm, "scale", 1.0),
			})
		}

		if len(nodes) > 0 {
			sensorList = append(sensorList, sensorNodeGroup{sensor: s, nodes: nodes})
		}
	}

	if len(sensorList) == 0 {
		log.Warn("No OPC-UA nodes configured — exiting")
		return
	}

	totalNodes := 0
	for _, sn := range sensorList {
		totalNodes += len(sn.nodes)
	}
	log.Infof("Polling %d nodes across %d sensors", totalNodes, len(sensorList))

	// ── Connect to OPC-UA server ─────────────────────────────────────────
	ctx := context.Background()

	opts := []opcua.Option{
		opcua.SecurityPolicy(securityPolicy),
	}
	switch securityMode {
	case "Sign":
		opts = append(opts, opcua.SecurityMode(ua.MessageSecurityModeSign))
	case "SignAndEncrypt":
		opts = append(opts, opcua.SecurityMode(ua.MessageSecurityModeSignAndEncrypt))
	default:
		opts = append(opts, opcua.SecurityMode(ua.MessageSecurityModeNone))
	}

	client, err := opcua.NewClient(endpoint, opts...)
	if err != nil {
		log.Fatalf("Failed to create OPC-UA client: %v", err)
	}
	if err := client.Connect(ctx); err != nil {
		log.Fatalf("Failed to connect to %s: %v", endpoint, err)
	}
	defer client.Close(ctx)

	log.Infof("Connected to OPC-UA server at %s", endpoint)

	// ── Core-switch client ───────────────────────────────────────────────
	csClient := coreswitch.NewClient(coreSwitchURL, "opcua-reader")

	// ── Poll loop ────────────────────────────────────────────────────────
	ticker := time.NewTicker(time.Duration(pollIntervalSec) * time.Second)
	defer ticker.Stop()

	pollOPCUA(log, ctx, client, csClient, sensorList, readerCfg.Name)
	for range ticker.C {
		pollOPCUA(log, ctx, client, csClient, sensorList, readerCfg.Name)
	}
}

func pollOPCUA(log interface{ Warnf(string, ...any); Debugf(string, ...any) },
	ctx context.Context, client *opcua.Client, csClient *coreswitch.Client,
	sensorList []sensorNodeGroup, readerName string) {

	for _, sn := range sensorList {
		var readings []coreswitch.DataIn
		tags := configs.FormatTags(sn.sensor.Tags)

		for _, node := range sn.nodes {
			id, err := ua.ParseNodeID(node.nodeID)
			if err != nil {
				log.Warnf("Invalid node ID %s: %v", node.nodeID, err)
				continue
			}

			req := &ua.ReadRequest{
				MaxAge: 2000,
				NodesToRead: []*ua.ReadValueID{
					{NodeID: id},
				},
			}

			resp, err := client.Read(ctx, req)
			if err != nil {
				log.Warnf("OPC-UA read failed for %s: %v", node.nodeID, err)
				continue
			}

			if len(resp.Results) == 0 || resp.Results[0].Status != ua.StatusOK {
				log.Warnf("OPC-UA bad status for %s: %v", node.nodeID, resp.Results[0].Status)
				continue
			}

			val := formatOPCUAValue(resp.Results[0].Value, node.scale)
			if val == "" {
				continue
			}

			readings = append(readings, coreswitch.DataIn{
				Table:     sn.sensor.Table,
				Equipment: sn.sensor.Name,
				Reading:   node.fieldKey,
				Output:    sn.sensor.Output,
				Sender:    readerName,
				Tags:      tags,
				Time:      time.Now().UnixMicro(),
				Value:     val,
			})
		}

		if len(readings) > 0 {
			if err := csClient.SendBatch(readings); err != nil {
				log.Warnf("Failed to send batch for %s: %v", sn.sensor.Name, err)
			} else {
				log.Debugf("Sent %d readings for %s", len(readings), sn.sensor.Name)
			}
		}
	}
}

func formatOPCUAValue(v *ua.Variant, scale float64) string {
	if v == nil {
		return ""
	}

	switch val := v.Value().(type) {
	case int8:
		return formatValue(float64(val) * scale)
	case int16:
		return formatValue(float64(val) * scale)
	case int32:
		return formatValue(float64(val) * scale)
	case int64:
		return formatValue(float64(val) * scale)
	case uint8:
		return formatValue(float64(val) * scale)
	case uint16:
		return formatValue(float64(val) * scale)
	case uint32:
		return formatValue(float64(val) * scale)
	case uint64:
		return formatValue(float64(val) * scale)
	case float32:
		return formatValue(float64(val) * scale)
	case float64:
		return formatValue(val * scale)
	case bool:
		if val {
			return "1"
		}
		return "0"
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
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
