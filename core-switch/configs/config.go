package configs

import (
	"os"
	"strconv"

	"github.com/sirupsen/logrus"
	corehttp "github.com/qube-enterprise/core-switch/http"
	"github.com/qube-enterprise/core-switch/influx"
	"github.com/qube-enterprise/pkg/sqliteconfig"
	_ "modernc.org/sqlite"
)

// Configs holds the complete core-switch v2 configuration.
type Configs struct {
	LogLevel string
	Http     corehttp.HttpCfg
	InfluxDB influx.InfluxCfg
	Live     corehttp.LiveCfg
	Alerts   corehttp.AlertCfg
}

var log *logrus.Logger

// LoadConfigs loads configuration from environment variables, with SQLite overrides
// when SQLITE_PATH is set. Falls back to defaults for all settings.
func LoadConfigs(l *logrus.Logger) *Configs {
	log = l

	conf := &Configs{
		LogLevel: getenv("LOG_LEVEL", "info"),
		Http: corehttp.HttpCfg{
			Port: envInt("HTTP_PORT", 8585),
		},
		InfluxDB: influx.InfluxCfg{
			Enabled: true,
			URL:     getenv("INFLUX_URL", "http://127.0.0.1:8086"),
			DB:      getenv("INFLUX_DB", "edgex"),
			User:    getenv("INFLUX_USER", "root"),
			Pass:    getenv("INFLUX_PASS", "root"),
		},
		Live: corehttp.LiveCfg{
			Enabled: false,
			URL:     getenv("CONF_AGENT_LIVE_URL", "http://enterprise-conf-agent:8585/v3/live"),
		},
		Alerts: corehttp.AlertCfg{
			IgnoreInterval: envInt("ALERTS_IGNORE_INTERVAL_SEC", 300),
		},
	}

	lvl, _ := logrus.ParseLevel(conf.LogLevel)
	log.SetLevel(lvl)

	// Load SQLite settings if SQLITE_PATH is set
	sqlitePath := os.Getenv("SQLITE_PATH")
	if sqlitePath != "" {
		applyFromSQLite(conf, sqlitePath)
	}

	log.Infof("Config loaded: influx=%s/%s http_port=%d live_enabled=%v",
		conf.InfluxDB.URL, conf.InfluxDB.DB, conf.Http.Port, conf.Live.Enabled)

	return conf
}

// applyFromSQLite overrides settings from the SQLite coreswitch_settings table.
func applyFromSQLite(conf *Configs, sqlitePath string) {
	db, err := sqliteconfig.OpenReadOnly(sqlitePath)
	if err != nil {
		log.Warnf("Cannot open SQLite at %s (using env defaults): %v", sqlitePath, err)
		return
	}
	defer db.Close()

	settings, err := sqliteconfig.LoadCoreSwitchSettings(db)
	if err != nil {
		log.Warnf("Cannot load coreswitch_settings from SQLite: %v", err)
		return
	}

	conf.InfluxDB.Enabled = settings.Outputs.InfluxDB
	conf.Live.Enabled = settings.Outputs.Live

	log.Infof("SQLite settings applied: outputs.influxdb=%v outputs.live=%v",
		settings.Outputs.InfluxDB, settings.Outputs.Live)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
