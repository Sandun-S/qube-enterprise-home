package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/qube-enterprise/core-switch/influx"
	"github.com/qube-enterprise/core-switch/mqtt"
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

var log *logrus.Logger
var alertCfg AlertCfg
var Last map[string]time.Time
var version string = "v3"

var dataPointsTotal prometheus.Counter
var dataPointsInflux prometheus.Counter
var dataPointsMQTT prometheus.Counter

func Init(l *logrus.Logger, server HttpCfg, al AlertCfg) {

	alertCfg = al
	log = l
	Last = make(map[string]time.Time, 0)

	dataPointsTotal = promauto.NewCounter(prometheus.CounterOpts{Name: "data_points_total"})
	dataPointsInflux = promauto.NewCounter(prometheus.CounterOpts{Name: "data_points_influx"})
	dataPointsMQTT = promauto.NewCounter(prometheus.CounterOpts{Name: "data_points_mqtt"})

	mux := http.NewServeMux()

	batchHndl := http.HandlerFunc(batchData)
	mux.Handle(fmt.Sprintf("/%s/batch", version), batchHndl)
	handle := http.HandlerFunc(singleData)
	mux.Handle(fmt.Sprintf("/%s/data", version), handle)
	alertHndl := http.HandlerFunc(alertRx)
	mux.Handle(fmt.Sprintf("/%s/alerts", version), alertHndl)
	mux.Handle("/metrics", promhttp.Handler())

	listen := fmt.Sprintf(":%d", server.Port)
	log.Println("Server listening on ", listen)

	http.ListenAndServe(listen, mux)
}

// batchData handles POST /v3/batch — batch has same Equipment.
func batchData(w http.ResponseWriter, r *http.Request) {

	readings := make([]schema.DataIn, 0)

	err := json.NewDecoder(r.Body).Decode(&readings)
	if err != nil {
		log.Error("JSON decode error: ", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var topic string
	inf := make([]*schema.DataIn, 0)
	mqt := make([]*schema.DataIn, 0)

	for i := range readings {
		m := readings[i]
		log.Debugf("%#v\n", m)

		if strings.Contains(m.Output, "influxdb") {
			inf = append(inf, &m)
		}
		if strings.Contains(m.Output, "mqtt") {
			mqt = append(mqt, &m)
			topic = fmt.Sprintf("%s.%s", m.Equipment, m.Reading)
		}
		if strings.Contains(m.Output, "live") {
			// v2: "live" output — forward to WebSocket via cloud API
			// For now, treat same as mqtt (readers can set Output="influxdb,live")
			mqt = append(mqt, &m)
			topic = fmt.Sprintf("%s.%s", m.Equipment, m.Reading)
		}
	}

	dataPointsTotal.Add(float64(len(readings)))
	dataPointsInflux.Add(float64(len(inf)))
	dataPointsMQTT.Add(float64(len(mqt)))

	if len(inf) > 0 {
		err = influx.BatchWrite(inf)
		if err != nil {
			log.Errorf("Could not write to influx - %s", err.Error())
		}
	}

	if len(mqt) > 0 {
		mesg, _ := json.Marshal(mqt)
		log.Debugf("MQTT MSG: %s - %s", topic, string(mesg[:]))
		mqtt.Send(topic, string(mesg[:]))
	}
}

func singleData(w http.ResponseWriter, r *http.Request) {

	reading := schema.DataIn{}

	err := json.NewDecoder(r.Body).Decode(&reading)
	if err != nil {
		log.Errorf("JSON decode error: %s", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var topic string
	inf := make([]*schema.DataIn, 0)
	mqt := make([]*schema.DataIn, 0)

	log.Debugf("%#v\n", reading)

	if strings.Contains(reading.Output, "influxdb") {
		inf = append(inf, &reading)
	}

	if strings.Contains(reading.Output, "mqtt") || strings.Contains(reading.Output, "live") {
		mqt = append(mqt, &reading)
		topic = fmt.Sprintf("%s.%s", reading.Equipment, reading.Reading)
	}

	dataPointsTotal.Inc()

	if len(inf) > 0 {
		err = influx.BatchWrite(inf)
		dataPointsInflux.Inc()
		if err != nil {
			log.Errorf("Could not write to influx - %s", err.Error())
		}
	}

	if len(mqt) > 0 {
		mesg, _ := json.Marshal(mqt)
		log.Debugf("MQTT MSG: %s - %s", topic, string(mesg[:]))
		mqtt.Send(topic, string(mesg[:]))
		dataPointsMQTT.Inc()
	}
}

func alertRx(w http.ResponseWriter, r *http.Request) {

	var alert schema.Alert

	err := json.NewDecoder(r.Body).Decode(&alert)
	if err != nil {
		log.Error("JSON decode error: ", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	key := fmt.Sprintf("%s:%s", alert.Sender, alert.Message)
	now := time.Now()
	prev, ok := Last[key]

	if ok {
		if now.Sub(prev).Seconds() < float64(alertCfg.IgnoreInterval) {
			log.Infof("Not sending repeated alert %#v", alert)
			return
		}
	}

	Last[key] = now

	ba, _ := json.Marshal(alert)
	alrt := string(ba[:])

	log.Infof("Alert received: %s", alrt)
	mqtt.Send("iot.platform.alerts", alrt)
}
