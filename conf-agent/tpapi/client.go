// Package tpapi provides the HTTP client and data types for TP-API communication.
// The TP-API is the device-facing API on port 8081 (HMAC authenticated).
// Analogous to v1 http/http.go but for TP-API instead of core-switch.
package tpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// ─── Data Types ──────────────────────────────────────────────────────────────

// SyncState is the response from GET /v1/sync/state.
type SyncState struct {
	Hash          string `json:"hash"`
	ConfigVersion int    `json:"config_version"`
	UpdatedAt     string `json:"updated_at"`
}

// SyncConfig is the full config payload from GET /v1/sync/config.
type SyncConfig struct {
	Hash               string              `json:"hash"`
	ConfigVersion      int                 `json:"config_version"`
	DockerComposeYML   string              `json:"docker_compose_yml"`
	Readers            []ReaderConfig      `json:"readers"`
	Containers         []ContainerConfig   `json:"containers"`
	CoreSwitchSettings map[string]string   `json:"coreswitch_settings"`
	TelemetrySettings  []map[string]any    `json:"telemetry_settings"`
}

// ReaderConfig is one reader from the sync config.
type ReaderConfig struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Protocol   string         `json:"protocol"`
	ConfigJSON map[string]any `json:"config_json"`
	Status     string         `json:"status"`
	Sensors    []SensorConfig `json:"sensors"`
}

// SensorConfig is one sensor within a reader config.
type SensorConfig struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	ConfigJSON map[string]any `json:"config_json"`
	TagsJSON   map[string]any `json:"tags_json"`
	Output     string         `json:"output"`
	TableName  string         `json:"table_name"`
}

// ContainerConfig is one container from the sync config.
type ContainerConfig struct {
	ID          string         `json:"id"`
	ReaderID    string         `json:"reader_id"`
	Image       string         `json:"image"`
	ServiceName string         `json:"service_name"`
	EnvJSON     map[string]any `json:"env_json"`
	Protocol    string         `json:"protocol"`
}

// WSMessage is a WebSocket message to/from the cloud.
type WSMessage struct {
	Type      string `json:"type"`
	QubeID    string `json:"qube_id"`
	ID        string `json:"id"`
	Payload   any    `json:"payload"`
	Timestamp int64  `json:"timestamp"`
}

// Command is a remote command from the cloud.
type Command struct {
	ID      string         `json:"id"`
	Command string         `json:"command"`
	Payload map[string]any `json:"payload"`
}

// PollResponse is the response from POST /v1/commands/poll.
type PollResponse struct {
	Commands []Command `json:"commands"`
}

// ─── HTTP Client ──────────────────────────────────────────────────────────────

// Client handles authenticated HTTP calls to the TP-API.
type Client struct {
	tpapiURL  string
	qubeID    string
	qubeToken string
	http      *http.Client
}

// NewClient creates a TP-API client.
func NewClient(tpapiURL, qubeID, qubeToken string) *Client {
	return &Client{
		tpapiURL:  tpapiURL,
		qubeID:    qubeID,
		qubeToken: qubeToken,
		http:      &http.Client{Timeout: 15 * time.Second},
	}
}

// Do makes an authenticated request (Qube token in header).
func (c *Client) Do(method, path string, body any) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.tpapiURL+path, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-Qube-ID", c.qubeID)
	req.Header.Set("Authorization", "Bearer "+c.qubeToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

// DoPublic makes an unauthenticated request (for device registration).
func (c *Client) DoPublic(method, path string, body any) ([]byte, int, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(method, c.tpapiURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

// WithAuth returns a new client with updated auth tokens.
func (c *Client) WithAuth(qubeID, qubeToken string) *Client {
	return NewClient(c.tpapiURL, qubeID, qubeToken)
}
