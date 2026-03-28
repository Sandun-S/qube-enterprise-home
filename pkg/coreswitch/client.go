// Package coreswitch provides an HTTP client for sending data to core-switch.
// This matches the existing core-switch v3 schema.DataIn format exactly.
//
// Usage:
//
//	client := coreswitch.NewClient("http://core-switch:8585")
//	err := client.SendBatch(readings)
package coreswitch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// DataIn matches core-switch/schema/schema.go exactly.
// All readers MUST use this struct when sending data to core-switch.
//
// Field rules:
//   - Output: "influxdb", "live", or "influxdb,live" (comma-separated, no spaces)
//   - Time:   Unix MICROSECONDS (time.Now().UnixMicro())
//   - Value:  Always a string, even for numeric values
//   - Tags:   Comma-separated key=value pairs: "name=PM5100,location=rack1"
type DataIn struct {
	Table     string `json:"Table"`
	Equipment string `json:"Equipment"`
	Reading   string `json:"Reading"`
	Output    string `json:"Output"`
	Sender    string `json:"Sender"`
	Tags      string `json:"Tags"`
	Time      int64  `json:"Time"`
	Value     string `json:"Value"`
}

// Alert matches core-switch/schema/schema.go exactly.
type Alert struct {
	Sender  string `json:"Sender"`
	Message string `json:"Message"`
	Type    string `json:"Type"` // "connectivity", "data", "other"
	Mode    int    `json:"Mode"` // 0=resolved, 1=complain
}

// Client sends data to core-switch via HTTP POST.
type Client struct {
	baseURL    string
	httpClient *http.Client
	sender     string // identifies this reader (e.g., "modbus-reader")
}

// NewClient creates a core-switch client.
// baseURL is typically "http://core-switch:8585".
// sender is the reader name (e.g., "modbus-reader", "snmp-reader").
func NewClient(baseURL string, sender string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		sender: sender,
	}
}

// SendBatch sends a batch of readings to POST /v3/batch.
// All readings in a batch should have the same Equipment field.
func (c *Client) SendBatch(readings []DataIn) error {
	if len(readings) == 0 {
		return nil
	}

	// Set sender and timestamp if not already set
	now := time.Now().UnixMicro()
	for i := range readings {
		if readings[i].Sender == "" {
			readings[i].Sender = c.sender
		}
		if readings[i].Time == 0 {
			readings[i].Time = now
		}
	}

	body, err := json.Marshal(readings)
	if err != nil {
		return fmt.Errorf("marshal batch: %w", err)
	}

	resp, err := c.httpClient.Post(
		c.baseURL+"/v3/batch",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("post batch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("core-switch returned %d", resp.StatusCode)
	}

	return nil
}

// SendSingle sends a single reading to POST /v3/data.
func (c *Client) SendSingle(reading DataIn) error {
	if reading.Sender == "" {
		reading.Sender = c.sender
	}
	if reading.Time == 0 {
		reading.Time = time.Now().UnixMicro()
	}

	body, err := json.Marshal(reading)
	if err != nil {
		return fmt.Errorf("marshal single: %w", err)
	}

	resp, err := c.httpClient.Post(
		c.baseURL+"/v3/data",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("post single: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("core-switch returned %d", resp.StatusCode)
	}

	return nil
}

// SendAlert sends an alert to POST /v3/alerts.
func (c *Client) SendAlert(alertType string, message string, mode int) error {
	alert := Alert{
		Sender:  c.sender,
		Message: message,
		Type:    alertType,
		Mode:    mode,
	}

	body, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("marshal alert: %w", err)
	}

	resp, err := c.httpClient.Post(
		c.baseURL+"/v3/alerts",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("post alert: %w", err)
	}
	defer resp.Body.Close()

	return nil
}
