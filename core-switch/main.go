// core-switch v2 — Qube Enterprise edge data router
//
// Accepts DataIn from all readers via HTTP POST /v3/batch and /v3/data.
// Routes to InfluxDB (edge buffer) and/or live forwarding (conf-agent → cloud WebSocket).
// MQTT output has been removed in v2 — use "live" output for real-time streaming.
//
// Env vars:
//   HTTP_PORT                  — Listen port [default: 8585]
//   INFLUX_URL                 — InfluxDB URL [default: http://127.0.0.1:8086]
//   INFLUX_DB                  — InfluxDB database [default: edgex]
//   INFLUX_USER                — InfluxDB username [default: root]
//   INFLUX_PASS                — InfluxDB password [default: root]
//   CONF_AGENT_LIVE_URL        — conf-agent live relay URL [default: http://enterprise-conf-agent:8585/v3/live]
//   ALERTS_IGNORE_INTERVAL_SEC — Seconds between repeated alerts [default: 300]
//   SQLITE_PATH                — Optional: path to edge SQLite (loads coreswitch_settings)
//   LOG_LEVEL                  — debug, info, warn, error [default: info]
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/Sandun-S/qube-enterprise-home/core-switch/configs"
	corehttp "github.com/Sandun-S/qube-enterprise-home/core-switch/http"
	"github.com/Sandun-S/qube-enterprise-home/core-switch/influx"
)

var log *logrus.Logger

type MyFormatter struct{}

func (f *MyFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	timestamp := entry.Time.Local().Format("2006-01-02 15:04:05.06")
	var b bytes.Buffer

	if entry.HasCaller() {
		fileVal := fmt.Sprintf("%s:%d", filepath.Base(entry.Caller.File), entry.Caller.Line)
		fmt.Fprintf(&b, "%s %s \"%s\" %s\n",
			timestamp, strings.ToUpper(entry.Level.String()), entry.Message, fileVal)
	} else {
		fmt.Fprintf(&b, "%s %s \"%s\"\n",
			timestamp, strings.ToUpper(entry.Level.String()), entry.Message)
	}

	return b.Bytes(), nil
}

func main() {
	log = logrus.New()
	log.SetReportCaller(true)
	log.SetFormatter(&MyFormatter{})
	log.SetOutput(os.Stdout)
	log.Print("Starting core-switch v2")

	conf := configs.LoadConfigs(log)

	influx.Init(log, conf.InfluxDB)

	corehttp.Init(log, conf.Http, conf.Alerts, conf.Live)
}
