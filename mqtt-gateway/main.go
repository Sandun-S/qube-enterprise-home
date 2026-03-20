// mqtt-gateway — Qube Enterprise Edge Service
// Reads topics.csv injected by Conf-Agent, subscribes to an MQTT broker,
// extracts values via JSON path, and HTTP POSTs to Coreswitch.
// Uses only stdlib + paho.mqtt.golang + gjson.
package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/tidwall/gjson"
)

// ─── Config ───────────────────────────────────────────────────────────────────

type Config struct {
	BrokerURL      string
	BaseTopic      string
	MQTTUsername   string
	MQTTPassword   string
	MQTTQOS        byte
	CSVPath        string
	CoreSwitchURL  string
	ServiceName    string
	ReconnectDelay time.Duration
}

func loadConfig() Config {
	qos, _ := strconv.Atoi(getenv("MQTT_QOS", "1"))
	return Config{
		BrokerURL:      getenv("MQTT_BROKER_URL", "tcp://localhost:1883"),
		BaseTopic:      getenv("MQTT_BASE_TOPIC", ""),
		MQTTUsername:   getenv("MQTT_USERNAME", ""),
		MQTTPassword:   getenv("MQTT_PASSWORD", ""),
		MQTTQOS:        byte(qos),
		CSVPath:        getenv("CSV_PATH", "/config/config.csv"),
		CoreSwitchURL:  getenv("CORESWITCH_URL", "http://coreswitch:8080"),
		ServiceName:    getenv("SERVICE_NAME", "mqtt-gateway"),
		ReconnectDelay: 10 * time.Second,
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ─── Topic Rule (one row from CSV) ───────────────────────────────────────────

type TopicRule struct {
	SensorName string // Column: SensorName
	Topic      string // Column: Topic
	JSONPath   string // Column: JSONPath   (gjson path, e.g. "data.voltage")
	FieldKey   string // Column: FieldKey
	Table      string // Column: Table
	Tags       string // Column: Tags
}

func loadCSV(path string) ([]TopicRule, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	// Build column index map (case-insensitive)
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	col := func(row []string, name string) string {
		if i, ok := idx[strings.ToLower(name)]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}

	var rules []TopicRule
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("[csv] skipping bad row: %v", err)
			continue
		}
		topic := col(row, "topic")
		if topic == "" {
			continue
		}
		rules = append(rules, TopicRule{
			SensorName: col(row, "sensorname"),
			Topic:      topic,
			JSONPath:   normalisePath(col(row, "jsonpath")),
			FieldKey:   col(row, "fieldkey"),
			Table:      col(row, "table"),
			Tags:       col(row, "tags"),
		})
	}
	log.Printf("[csv] loaded %d topic rules from %s", len(rules), path)
	return rules, nil
}

// normalisePath strips leading "$." from gjson paths coming from the CSV.
func normalisePath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, "$[")
	return path
}

// ─── Coreswitch Payload ───────────────────────────────────────────────────────

type Reading struct {
	SensorName string            `json:"sensor_name"`
	Table      string            `json:"table"`
	Tags       map[string]string `json:"tags"`
	Fields     map[string]any    `json:"fields"`
	Timestamp  int64             `json:"timestamp_ms"`
}

// ─── Gateway ─────────────────────────────────────────────────────────────────

type Gateway struct {
	cfg    Config
	rules  []TopicRule
	mu     sync.RWMutex
	client *http.Client
}

func newGateway(cfg Config, rules []TopicRule) *Gateway {
	return &Gateway{
		cfg:   cfg,
		rules: rules,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (g *Gateway) updateRules(rules []TopicRule) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rules = rules
}

func (g *Gateway) matchingRules(topic string) []TopicRule {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var matched []TopicRule
	for _, r := range g.rules {
		if mqttTopicMatch(r.Topic, topic) {
			matched = append(matched, r)
		}
	}
	return matched
}

// handleMessage is called by paho for every incoming MQTT message.
func (g *Gateway) handleMessage(_ mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	payload := msg.Payload()

	rules := g.matchingRules(topic)
	if len(rules) == 0 {
		return
	}

	// Parse payload as JSON once
	jsonStr := string(payload)
	isJSON := gjson.Valid(jsonStr)

	// Group rules by sensor name (one POST per sensor per message)
	type sensorKey struct{ name, table, tags string }
	grouped := map[sensorKey]map[string]any{}

	for _, rule := range rules {
		var value any

		if rule.JSONPath == "" || rule.JSONPath == "." {
			// Whole payload as string
			value = string(payload)
		} else if isJSON {
			result := gjson.Get(jsonStr, rule.JSONPath)
			if !result.Exists() {
				log.Printf("[mqtt] path %q not found in message on topic %s", rule.JSONPath, topic)
				continue
			}
			value = result.Value()
		} else {
			// Non-JSON payload — use raw string
			value = string(payload)
		}

		sk := sensorKey{name: rule.SensorName, table: rule.Table, tags: rule.Tags}
		if grouped[sk] == nil {
			grouped[sk] = map[string]any{}
		}
		grouped[sk][rule.FieldKey] = value
	}

	// Send one reading per sensor
	for sk, fields := range grouped {
		reading := Reading{
			SensorName: sk.name,
			Table:      sk.table,
			Tags:       parseTags(sk.tags),
			Fields:     fields,
			Timestamp:  time.Now().UnixMilli(),
		}
		if err := g.postToCoreswitch(reading); err != nil {
			log.Printf("[coreswitch] POST failed for %s: %v", sk.name, err)
		} else {
			log.Printf("[coreswitch] sent %s fields=%v", sk.name, fieldKeys(fields))
		}
	}
}

func (g *Gateway) postToCoreswitch(reading Reading) error {
	body, err := json.Marshal(reading)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	resp, err := g.client.Post(g.cfg.CoreSwitchURL+"/ingest", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("coreswitch returned %d: %s", resp.StatusCode, b)
	}
	return nil
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	cfg := loadConfig()
	log.Printf("[mqtt-gateway] starting — broker=%s csv=%s coreswitch=%s",
		cfg.BrokerURL, cfg.CSVPath, cfg.CoreSwitchURL)

	rules, err := loadCSV(cfg.CSVPath)
	if err != nil {
		log.Fatalf("[csv] failed to load: %v", err)
	}

	gw := newGateway(cfg, rules)

	// Build unique topic subscription list from rules
	topics := uniqueTopics(rules)

	opts := mqtt.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID("qube-mqtt-gateway-" + cfg.ServiceName).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(cfg.ReconnectDelay).
		SetOnConnectHandler(func(c mqtt.Client) {
			log.Printf("[mqtt] connected to %s", cfg.BrokerURL)
			// Subscribe to all topics
			filters := map[string]byte{}
			for _, t := range topics {
				filters[t] = cfg.MQTTQOS
				log.Printf("[mqtt] subscribing: %s", t)
			}
			if len(filters) > 0 {
				token := c.SubscribeMultiple(filters, gw.handleMessage)
				token.Wait()
				if token.Error() != nil {
					log.Printf("[mqtt] subscribe error: %v", token.Error())
				}
			}
		}).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			log.Printf("[mqtt] connection lost: %v — will reconnect", err)
		})

	if cfg.MQTTUsername != "" {
		opts.SetUsername(cfg.MQTTUsername)
	}
	if cfg.MQTTPassword != "" {
		opts.SetPassword(cfg.MQTTPassword)
	}

	client := mqtt.NewClient(opts)
	for {
		token := client.Connect()
		token.Wait()
		if token.Error() == nil {
			break
		}
		log.Printf("[mqtt] connect failed: %v — retrying in %s", token.Error(), cfg.ReconnectDelay)
		time.Sleep(cfg.ReconnectDelay)
	}

	log.Printf("[mqtt-gateway] running — subscribed to %d topics", len(topics))

	// Watch CSV for changes (Conf-Agent may update it)
	go watchCSV(cfg.CSVPath, gw, client, cfg)

	// Block forever
	select {}
}

// watchCSV polls the CSV file every 30s. If it changes (by size/mtime),
// it reloads the rules and re-subscribes.
func watchCSV(path string, gw *Gateway, client mqtt.Client, cfg Config) {
	var lastMod time.Time
	var lastSize int64
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.ModTime() == lastMod && info.Size() == lastSize {
			continue
		}
		lastMod = info.ModTime()
		lastSize = info.Size()

		newRules, err := loadCSV(path)
		if err != nil {
			log.Printf("[csv-watch] reload error: %v", err)
			continue
		}

		// Unsubscribe old topics, subscribe new
		oldTopics := uniqueTopics(gw.rules)
		newTopics := uniqueTopics(newRules)

		for _, t := range oldTopics {
			client.Unsubscribe(t)
		}
		gw.updateRules(newRules)
		filters := map[string]byte{}
		for _, t := range newTopics {
			filters[t] = cfg.MQTTQOS
		}
		if len(filters) > 0 {
			client.SubscribeMultiple(filters, gw.handleMessage)
		}
		log.Printf("[csv-watch] reloaded — %d rules, %d topics", len(newRules), len(newTopics))
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// uniqueTopics extracts deduplicated MQTT topics from rules.
func uniqueTopics(rules []TopicRule) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rules {
		if !seen[r.Topic] {
			seen[r.Topic] = true
			out = append(out, r.Topic)
		}
	}
	return out
}

// mqttTopicMatch checks whether a concrete topic matches an MQTT filter
// (supports + and # wildcards).
func mqttTopicMatch(filter, topic string) bool {
	if filter == topic {
		return true
	}
	fp := strings.Split(filter, "/")
	tp := strings.Split(topic, "/")
	return matchSegments(fp, tp)
}

func matchSegments(fp, tp []string) bool {
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

// parseTags converts "key=value,key2=value2" into a map.
func parseTags(raw string) map[string]string {
	tags := map[string]string{}
	if raw == "" {
		return tags
	}
	for _, pair := range strings.Split(raw, ",") {
		parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(parts) == 2 {
			tags[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return tags
}

func fieldKeys(fields map[string]any) []string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	return keys
}
