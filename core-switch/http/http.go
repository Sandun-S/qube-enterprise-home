package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/qube-enterprise/core-switch/influx"
	"github.com/qube-enterprise/core-switch/schema"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type HttpCfg struct {
	Port int `yaml:"Port"`
}

type AlertCfg struct {
	IgnoreInterval int `yaml:"IgnoreInterval"`
}

// LiveCfg controls the "live" output forwarding to conf-agent.
type LiveCfg struct {
	Enabled bool
	URL     string
}

var log *logrus.Logger
var alertCfg AlertCfg
var liveCfg LiveCfg
var Last map[string]time.Time
var version string = "v3"

var dataPointsTotal prometheus.Counter
var dataPointsInflux prometheus.Counter
var dataPointsLive prometheus.Counter

func Init(l *logrus.Logger, server HttpCfg, al AlertCfg, live LiveCfg) {

	alertCfg = al
	liveCfg = live
	log = l
	Last = make(map[string]time.Time)

	dataPointsTotal = promauto.NewCounter(prometheus.CounterOpts{Name: "data_points_total"})
	dataPointsInflux = promauto.NewCounter(prometheus.CounterOpts{Name: "data_points_influx"})
	dataPointsLive = promauto.NewCounter(prometheus.CounterOpts{Name: "data_points_live"})

	mux := http.NewServeMux()

	mux.Handle(fmt.Sprintf("/%s/batch", version), http.HandlerFunc(batchData))
	mux.Handle(fmt.Sprintf("/%s/data", version), http.HandlerFunc(singleData))
	mux.Handle(fmt.Sprintf("/%s/alerts", version), http.HandlerFunc(alertRx))
	mux.Handle("/metrics", promhttp.Handler())

	listen := fmt.Sprintf(":%d", server.Port)
	log.Println("Server listening on ", listen)

	http.ListenAndServe(listen, mux)
}

// batchData handles POST /v3/batch.
func batchData(w http.ResponseWriter, r *http.Request) {

	var readings []schema.DataIn

	if err := json.NewDecoder(r.Body).Decode(&readings); err != nil {
		log.Error("JSON decode error: ", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	inf := make([]*schema.DataIn, 0)
	live := make([]*schema.DataIn, 0)

	for i := range readings {
		m := &readings[i]
		log.Debugf("%#v", m)

		if strings.Contains(m.Output, "influxdb") {
			inf = append(inf, m)
		}
		if strings.Contains(m.Output, "live") {
			live = append(live, m)
		}
	}

	dataPointsTotal.Add(float64(len(readings)))
	dataPointsInflux.Add(float64(len(inf)))
	dataPointsLive.Add(float64(len(live)))

	if len(inf) > 0 {
		if err := influx.BatchWrite(inf); err != nil {
			log.Errorf("Could not write to influx: %v", err)
		}
	}

	if len(live) > 0 {
		forwardLive(live)
	}
}

// singleData handles POST /v3/data.
func singleData(w http.ResponseWriter, r *http.Request) {

	var reading schema.DataIn

	if err := json.NewDecoder(r.Body).Decode(&reading); err != nil {
		log.Errorf("JSON decode error: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	log.Debugf("%#v", reading)

	dataPointsTotal.Inc()

	if strings.Contains(reading.Output, "influxdb") {
		if err := influx.BatchWrite([]*schema.DataIn{&reading}); err != nil {
			log.Errorf("Could not write to influx: %v", err)
		}
		dataPointsInflux.Inc()
	}

	if strings.Contains(reading.Output, "live") {
		forwardLive([]*schema.DataIn{&reading})
		dataPointsLive.Inc()
	}
}

// alertRx handles POST /v3/alerts.
func alertRx(w http.ResponseWriter, r *http.Request) {

	var alert schema.Alert

	if err := json.NewDecoder(r.Body).Decode(&alert); err != nil {
		log.Error("JSON decode error: ", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	key := fmt.Sprintf("%s:%s", alert.Sender, alert.Message)
	now := time.Now()
	prev, ok := Last[key]

	if ok {
		if now.Sub(prev).Seconds() < float64(alertCfg.IgnoreInterval) {
			log.Infof("Suppressing repeated alert from %s", alert.Sender)
			return
		}
	}

	Last[key] = now

	ba, _ := json.Marshal(alert)
	log.Infof("Alert received: %s", string(ba))

	// Alerts are logged and optionally forwarded to conf-agent for cloud relay.
	if liveCfg.Enabled && liveCfg.URL != "" {
		go func() {
			resp, err := http.DefaultClient.Post(liveCfg.URL+"/alert", "application/json", bytes.NewReader(ba))
			if err != nil {
				log.Debugf("Alert forward failed: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}
}

// forwardLive forwards "live" data points to conf-agent for cloud WebSocket relay.
// If live forwarding is disabled or conf-agent is unreachable, data is logged and dropped.
func forwardLive(readings []*schema.DataIn) {
	if !liveCfg.Enabled || liveCfg.URL == "" {
		log.Debugf("Live output: %d point(s) — forwarding disabled, dropping", len(readings))
		return
	}

	body, err := json.Marshal(readings)
	if err != nil {
		log.Errorf("Live forward: marshal error: %v", err)
		return
	}

	go func() {
		resp, err := http.DefaultClient.Post(liveCfg.URL, "application/json", bytes.NewReader(body))
		if err != nil {
			log.Debugf("Live forward to %s failed: %v", liveCfg.URL, err)
			return
		}
		resp.Body.Close()
		log.Debugf("Live: forwarded %d point(s) to conf-agent", len(readings))
	}()
}
