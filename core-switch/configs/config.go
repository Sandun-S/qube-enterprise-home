package configs

import (
	"os"

	"github.com/sirupsen/logrus"
	"github.com/qube-enterprise/core-switch/http"
	"github.com/qube-enterprise/core-switch/influx"
	"github.com/qube-enterprise/core-switch/mqtt"

	"gopkg.in/yaml.v2"
)

// Configs holds the complete core-switch configuration.
type Configs struct {
	LogLevel string           `yaml:"LogLevel"`
	Http     http.HttpCfg     `yaml:"HTTP"`
	InfluxDB influx.InfluxCfg `yaml:"InfluxDB"`
	MQTT     mqtt.MQTTCfg     `yaml:"MQTT"`
	Alerts   http.AlertCfg    `yaml:"Alerts"`
}

var log *logrus.Logger

func LoadConfigs(l *logrus.Logger, confile string) *Configs {

	log = l

	fd, err := os.Open(confile)
	if err != nil {
		log.Fatalf("Cannot open config file %s - %s", confile, err.Error())
	}
	defer fd.Close()

	decoder := yaml.NewDecoder(fd)
	conf := &Configs{}
	err = decoder.Decode(conf)

	if err != nil {
		log.Fatal("Config file parse error ", err)
	}

	lvl, _ := logrus.ParseLevel(conf.LogLevel)
	log.SetLevel(lvl)

	log.Infof("Configs loaded : %#v", *conf)

	return conf
}
