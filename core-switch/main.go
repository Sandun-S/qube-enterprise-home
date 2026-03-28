package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/qube-enterprise/core-switch/configs"
	"github.com/qube-enterprise/core-switch/http"
	"github.com/qube-enterprise/core-switch/influx"
	"github.com/qube-enterprise/core-switch/mqtt"
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

	confFile := flag.String("configs", "configs.yml", "Configuration file for core-switch")
	flag.Parse()

	log = logrus.New()
	log.SetReportCaller(true)
	log.SetFormatter(&MyFormatter{})
	log.SetOutput(os.Stdout)
	log.Print("Starting core-switch v2")

	conf := configs.LoadConfigs(log, *confFile)

	influx.Init(log, conf.InfluxDB)
	mqtt.Init(log, conf.MQTT)
	http.Init(log, conf.Http, conf.Alerts)
}
