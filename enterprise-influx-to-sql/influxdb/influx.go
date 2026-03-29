// Package influxdb queries InfluxDB v1 for aggregated sensor data.
// Each call to QueryTable creates and closes its own HTTP client — no persistent connection.
package influxdb

import (
	"fmt"
	"strconv"
	"time"

	client "github.com/influxdata/influxdb1-client/v2"
	"github.com/qube-enterprise/enterprise-influx-to-sql/configs"
	"github.com/qube-enterprise/enterprise-influx-to-sql/schema"
)

// Client wraps InfluxDB connection settings.
type Client struct {
	cfg configs.InfluxConfig
}

// New creates a new Client. Call Ping() to verify connectivity before use.
func New(cfg configs.InfluxConfig) *Client {
	return &Client{cfg: cfg}
}

// Ping checks that InfluxDB is reachable and responding.
func (c *Client) Ping() error {
	cl, err := c.newHTTPClient()
	if err != nil {
		return err
	}
	defer cl.Close()
	_, _, err = cl.Ping(5 * time.Second)
	return err
}

// QueryTable queries a single InfluxDB measurement (table) for the given time range.
// It groups by 1-minute intervals and by the `device` and `reading` tags — exactly
// how core-switch writes data via line protocol.
//
// Returns one RawRecord per (device, reading, 1m-bucket) with a mean(value).
func (c *Client) QueryTable(table string, start, end time.Time) ([]schema.RawRecord, error) {
	const layout = "2006-01-02 15:04:05"

	q := fmt.Sprintf(
		`SELECT mean(value) FROM "%s" WHERE time >= '%s' AND time < '%s' GROUP BY time(1m), device, reading ORDER BY time ASC`,
		table,
		start.Format(layout),
		end.Format(layout),
	)

	cl, err := c.newHTTPClient()
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}
	defer cl.Close()

	resp, err := cl.Query(client.Query{Command: q, Database: c.cfg.DB})
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	if resp.Error() != nil {
		return nil, fmt.Errorf("response: %w", resp.Error())
	}

	var recs []schema.RawRecord
	for _, r := range resp.Results {
		for _, sr := range r.Series {
			device := sr.Tags["device"]
			reading := sr.Tags["reading"]
			for _, vals := range sr.Values {
				if len(vals) < 2 || vals[1] == nil {
					continue
				}
				// vals[0] is the timestamp string, vals[1] is the aggregated value
				t, _ := time.Parse("2006-01-02T15:04:05Z", fmt.Sprintf("%v", vals[0]))
				v, _ := strconv.ParseFloat(fmt.Sprintf("%v", vals[1]), 64)
				recs = append(recs, schema.RawRecord{
					Time:      t,
					Equipment: device,
					Reading:   reading,
					Value:     v,
				})
			}
		}
	}

	return recs, nil
}

func (c *Client) newHTTPClient() (client.Client, error) {
	return client.NewHTTPClient(client.HTTPConfig{
		Addr:     c.cfg.URL,
		Username: c.cfg.User,
		Password: c.cfg.Pass,
	})
}
