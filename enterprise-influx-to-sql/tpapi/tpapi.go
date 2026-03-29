// Package tpapi is the client for the Enterprise TP-API telemetry ingest endpoint.
package tpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/qube-enterprise/enterprise-influx-to-sql/configs"
	"github.com/qube-enterprise/enterprise-influx-to-sql/schema"
)

// Client sends batches of readings to the Enterprise TP-API.
type Client struct {
	cfg  configs.TPAPIConfig
	http *http.Client
}

// New creates a TP-API client with a 30s timeout.
func New(cfg configs.TPAPIConfig) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// PostReadings sends a batch of readings to POST /v1/telemetry/ingest.
// Uses X-Qube-ID header + Bearer token for HMAC-equivalent auth.
func (c *Client) PostReadings(readings []schema.Reading) error {
	body, err := json.Marshal(map[string]any{"readings": readings})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest("POST", c.cfg.URL+"/v1/telemetry/ingest", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Qube-ID", c.cfg.QubeID)
	req.Header.Set("Authorization", "Bearer "+c.cfg.QubeToken)

	resp, err := c.http.Do(req)
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
