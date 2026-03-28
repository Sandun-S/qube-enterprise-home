package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/sirupsen/logrus"
)

type HTTPConfig struct {
	Data   string `yaml:"Data"`
	Alerts string `yaml:"Alerts"`
}

type Alert struct {
	Sender  string `json:"Sender"`
	Message string `json:"Message"`
	Type    string `json:"Type"`
	Mode    int    `json:"Mode"`
}

var httpConfig *HTTPConfig
var log *logrus.Logger

func Init(l *logrus.Logger, conf *HTTPConfig) {
	httpConfig = conf
	log = l
}

func POSTData(rec []byte) error {
	req, err := http.NewRequest("POST", httpConfig.Data, bytes.NewBuffer(rec))
	if err != nil {
		return err
	}

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	_, err = io.ReadAll(response.Body)
	return err
}

func SendAlert(alert *Alert) error {
	jsn, _ := json.Marshal(alert)

	req, err := http.NewRequest("POST", httpConfig.Alerts, bytes.NewBuffer(jsn))
	if err != nil {
		log.Error("Error sending alert to core-switch:", err.Error())
		return err
	}

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Error("Error sending alert to core-switch:", err.Error())
		return err
	}
	defer response.Body.Close()

	_, err = io.ReadAll(response.Body)
	return err
}
