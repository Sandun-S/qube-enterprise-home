// mqtt-reader — Qube Enterprise MQTT Reader (v2)
//
// Reads config from shared SQLite → subscribes to MQTT topics → extracts values
// via JSON path → POSTs to coreswitch.
//
// Config changes are handled by conf-agent stopping this container; Swarm recreates
// it and the new instance reads fresh config from SQLite on startup.
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

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/Sandun-S/qube-enterprise-home/mqtt-reader/coreswitch"
	"github.com/Sandun-S/qube-enterprise-home/mqtt-reader/logger"
	"github.com/Sandun-S/qube-enterprise-home/mqtt-reader/configs"
	"github.com/tidwall/gjson"
)

// topicRule maps one sensor config entry to an MQTT subscription.
type topicRule struct {
	sensor   configs.SensorConfig
	topic    string
	jsonPath string
	fieldKey string
}

func main() {
	log := logger.New("mqtt-reader")

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
	// Support both broker_url (full URL) and broker_host+broker_port (split form from UI)
	brokerURL := getString(readerCfg.Config, "broker_url", "")
	if brokerURL == "" {
		host := getString(readerCfg.Config, "broker_host", "localhost")
		port := getInt(readerCfg.Config, "broker_port", 1883)
		brokerURL = fmt.Sprintf("tcp://%s:%d", host, port)
	}
	username := getString(readerCfg.Config, "username", "")
	password := getString(readerCfg.Config, "password", "")
	clientID := getString(readerCfg.Config, "client_id", "")
	qos := getInt(readerCfg.Config, "qos", 1)

	log.Infof("MQTT broker: %s (qos=%d)", brokerURL, qos)

	// ── Build topic rules from sensors ───────────────────────────────────
	var rules []topicRule
	for _, s := range sensors {
		paths, ok := s.Config["json_paths"].([]any)
		if !ok {
			log.Warnf("Sensor %s has no json_paths array", s.Name)
			continue
		}

		// A top-level "topic" in sensor config acts as default for all json_paths entries
		// that don't specify their own topic. This is the standard pattern when topic is
		// a per-device parameter (set in device template sensor_params_schema).
		defaultTopic := getString(s.Config, "topic", "")

		for _, p := range paths {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			topic := getString(pm, "topic", defaultTopic)
			if topic == "" {
				log.Warnf("Sensor %s: json_paths entry has no topic and no default topic — skipping", s.Name)
				continue
			}
			rules = append(rules, topicRule{
				sensor:   s,
				topic:    topic,
				jsonPath: normalisePath(getString(pm, "json_path", ".")),
				fieldKey: getString(pm, "field_key", "value"),
			})
		}
	}

	if len(rules) == 0 {
		log.Warn("No topic rules configured — exiting")
		return
	}

	// ── Core-switch client ───────────────────────────────────────────────
	csClient := coreswitch.NewClient(coreSwitchURL, "mqtt-reader")

	// ── Connect to MQTT broker ───────────────────────────────────────────
	topics := uniqueTopics(rules)

	resolvedClientID := clientID
	if resolvedClientID == "" {
		resolvedClientID = fmt.Sprintf("qube-mqtt-reader-%s", readerID[:8])
	}

	opts := mqtt.NewClientOptions().
		AddBroker(brokerURL).
		SetClientID(resolvedClientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(10 * time.Second).
		SetOnConnectHandler(func(c mqtt.Client) {
			log.Infof("Connected to MQTT broker %s", brokerURL)
			filters := map[string]byte{}
			for _, t := range topics {
				filters[t] = byte(qos)
				log.Infof("Subscribing: %s", t)
			}
			if len(filters) > 0 {
				token := c.SubscribeMultiple(filters, func(_ mqtt.Client, msg mqtt.Message) {
					handleMessage(log, csClient, rules, readerCfg.Name, msg)
				})
				token.Wait()
				if token.Error() != nil {
					log.Warnf("Subscribe error: %v", token.Error())
				}
			}
		}).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			log.Warnf("MQTT connection lost: %v — will reconnect", err)
		})

	if username != "" {
		opts.SetUsername(username)
	}
	if password != "" {
		opts.SetPassword(password)
	}

	client := mqtt.NewClient(opts)
	for {
		token := client.Connect()
		token.Wait()
		if token.Error() == nil {
			break
		}
		log.Warnf("MQTT connect failed: %v — retrying in 10s", token.Error())
		time.Sleep(10 * time.Second)
	}

	log.Infof("Running — subscribed to %d topics for %d rules", len(topics), len(rules))

	// Block forever
	select {}
}

func handleMessage(log interface{ Debugf(string, ...any); Warnf(string, ...any) },
	csClient *coreswitch.Client, rules []topicRule, readerName string, msg mqtt.Message) {

	topic := msg.Topic()
	payload := string(msg.Payload())

	if !gjson.Valid(payload) {
		return
	}

	// Group by sensor
	type sensorKey struct {
		name, table, output, tags string
	}
	grouped := map[sensorKey][]coreswitch.DataIn{}

	for _, rule := range rules {
		if !mqttTopicMatch(rule.topic, topic) {
			continue
		}

		var value string
		if rule.jsonPath == "" || rule.jsonPath == "." {
			value = payload
		} else {
			result := gjson.Get(payload, rule.jsonPath)
			if !result.Exists() {
				continue
			}
			value = result.String()
		}

		tags := configs.FormatTags(rule.sensor.Tags)
		sk := sensorKey{
			name:   rule.sensor.Name,
			table:  rule.sensor.Table,
			output: rule.sensor.Output,
			tags:   tags,
		}

		reading := coreswitch.DataIn{
			Table:     rule.sensor.Table,
			Equipment: rule.sensor.Name,
			Reading:   rule.fieldKey,
			Output:    rule.sensor.Output,
			Sender:    readerName,
			Tags:      tags,
			Time:      time.Now().UnixMicro(),
			Value:     value,
		}
		grouped[sk] = append(grouped[sk], reading)
	}

	for _, readings := range grouped {
		if err := csClient.SendBatch(readings); err != nil {
			log.Warnf("Failed to send batch: %v", err)
		} else {
			log.Debugf("Sent %d readings from topic %s", len(readings), topic)
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

func uniqueTopics(rules []topicRule) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rules {
		if !seen[r.topic] {
			seen[r.topic] = true
			out = append(out, r.topic)
		}
	}
	return out
}

func mqttTopicMatch(filter, topic string) bool {
	if filter == topic {
		return true
	}
	fp := strings.Split(filter, "/")
	tp := strings.Split(topic, "/")
	for i, f := range fp {
		if f == "#" {
			return true
		}
		if i >= len(tp) {
			return false
		}
		if f != "+" && f != tp[i] {
			return false
		}
	}
	return len(fp) == len(tp)
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
