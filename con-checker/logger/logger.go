// Package logger provides standard logrus-based logging for con-checker.
// Embedded from pkg/logger — copied here so con-checker is self-contained.
package logger

import (
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

// New creates a logrus logger with the standard Qube Enterprise format.
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
