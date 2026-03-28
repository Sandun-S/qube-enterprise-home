package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/qube-enterprise/modbus-reader/configs"
	modbuspkg "github.com/qube-enterprise/modbus-reader/modbus"
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

	confFile := flag.String("config", "configs.yml", "Configuration file for modbus-reader")
	flag.Parse()

	log = logrus.New()
	log.SetReportCaller(true)
	log.SetFormatter(&MyFormatter{})
	log.SetOutput(os.Stdout)
	log.Info("Starting modbus-reader v2")

	conf, recs := configs.Load(log, *confFile)

	modbuspkg.Init(log, recs, &conf.Modbus, &conf.Http)

	var wait sync.WaitGroup
	wait.Add(1)
	wait.Wait()
}
