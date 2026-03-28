// Package logger provides standard logrus-based logging for all Qube Enterprise services.
// Matches the logging pattern used by existing gateways (core-switch, snmp-gateway, etc.).
//
// Usage:
//
//	log := logger.New("modbus-reader")
//	log.Info("Starting reader")
//	log.Debugf("Loaded %d sensors", len(sensors))
package logger

import (
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

// New creates a logrus logger with the standard Qube Enterprise format.
// serviceName is used as a field in log output (e.g., "modbus-reader").
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
