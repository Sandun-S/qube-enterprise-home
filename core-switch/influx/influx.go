package influx

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/Sandun-S/qube-enterprise-home/core-switch/schema"
)

type InfluxCfg struct {
	Enabled bool   `yaml:"Enabled"`
	URL     string `yaml:"URL"`
	DB      string `yaml:"DB"`
	User    string `yaml:"User"`
	Pass    string `yaml:"Pass"`
}

var conf InfluxCfg
var log *logrus.Logger
var writeLock sync.Mutex

func Init(l *logrus.Logger, c InfluxCfg) {
	log = l
	conf = c
}

// BatchWrite writes readings to InfluxDB using line protocol.
func BatchWrite(readings []*schema.DataIn) error {

	if !conf.Enabled {
		return nil
	}

	writeLock.Lock()
	defer writeLock.Unlock()

	url := fmt.Sprintf("%s/write?db=%s&p=%s&precision=u&rp=&u=%s", conf.URL, conf.DB, conf.Pass, conf.User)

	var data string

	for _, m := range readings {
		if m.Tags == "" {
			data += fmt.Sprintf("%s,device=%s,reading=%s value=%s %d\n", m.Table, m.Equipment, m.Reading, m.Value, m.Time)
		} else {
			data += fmt.Sprintf("%s,device=%s,reading=%s,%s value=%s %d\n", m.Table, m.Equipment, m.Reading, m.Tags, m.Value, m.Time)
		}
	}

	if len(data) == 0 {
		return nil
	}

	log.Debug(url + "\n" + data)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(data)))
	if err != nil {
		return err
	}

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	res, err := io.ReadAll(response.Body)

	if len(res) > 0 {
		log.Debugf("%#v", string(res[:]))
		log.Debugf("data:%s", data)
	}

	return err
}
