// Package schema defines the data types used throughout enterprise-influx-to-sql.
package schema

import "time"

// Reading is the telemetry record POSTed to the Enterprise TP-API.
type Reading struct {
	Time     time.Time `json:"time"`
	SensorID string    `json:"sensor_id"`
	FieldKey string    `json:"field_key"`
	Value    float64   `json:"value"`
	Unit     string    `json:"unit"`
}

// RawRecord is a single measurement read from InfluxDB.
// Equipment and Reading match the `device` and `reading` tags written by core-switch.
type RawRecord struct {
	Time      time.Time
	Equipment string // InfluxDB tag: device
	Reading   string // InfluxDB tag: reading
	Value     float64
}

// SensorMap maps "Equipment.Reading" (or "Equipment") to a cloud sensor UUID.
// Both dots and colons are accepted as separators when loading from different sources.
type SensorMap map[string]string
