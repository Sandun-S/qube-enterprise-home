// Package logger provides standard logrus-based logging for snmp-reader.
// Matches the logging pattern used by all Qube Enterprise services.
//
// Embedded from pkg/logger — copied here so snmp-reader is self-contained
// and can live in its own repo (gitlab.com/iot-team4/product/qube/snmp-reader).
//
// Usage:
//
//	log := logger.New("snmp-reader")
//	log.Info("Starting reader")
//	log.Debugf("Loaded %d devices", len(devices))
package logger

import (
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

// New creates a logrus logger with the standard Qube Enterprise format.
// serviceName is used as a field in log output (e.g., "snmp-reader").
// Log level is read from LOG_LEVEL env var (default: "info").
func New(serviceName string) *logrus.Logger {
	log := logrus.New()
	log.SetOutput(os.Stdout)
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	level := os.Getenv("LOG_LEVEL")
	if level == "" {
		level = "info"
	}

	switch strings.ToLower(level) {
	case "debug":
		log.SetLevel(logrus.DebugLevel)
	case "warn", "warning":
		log.SetLevel(logrus.WarnLevel)
	case "error":
		log.SetLevel(logrus.ErrorLevel)
	default:
		log.SetLevel(logrus.InfoLevel)
	}

	log.WithField("service", serviceName).Info("Logger initialized")

	return log
}
