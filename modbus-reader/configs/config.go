package configs

import (
	"database/sql"
	"encoding/json"
	"os"
	"strconv"

	"github.com/sirupsen/logrus"
	"github.com/qube-enterprise/modbus-reader/modbus"
	"gopkg.in/yaml.v2"

	_ "modernc.org/sqlite"
)

type Config struct {
	LogLevel string              `yaml:"LogLevel"`
	Modbus   modbus.ModbusConfig `yaml:"Modbus"`
	Http     HttpConfig          `yaml:"HTTP"`
}

type HttpConfig struct {
	Data   string `yaml:"Data"`
	Alerts string `yaml:"Alerts"`
}

var log *logrus.Logger

// Load loads config from YAML + readings from SQLite (v2) or CSV fallback.
func Load(l *logrus.Logger, confFile string) (*Config, []*modbus.Reading) {

	log = l

	conf := &Config{}

	// Check if YAML config exists (production mode)
	if _, err := os.Stat(confFile); err == nil {
		fd, err := os.Open(confFile)
		if err != nil {
			log.Fatalf("Cannot open config file %s - %s", confFile, err.Error())
		}
		defer fd.Close()

		decoder := yaml.NewDecoder(fd)
		err = decoder.Decode(conf)
		if err != nil {
			log.Fatal("Config file parse error ", err)
		}
	}

	// Override from env vars (v2 mode — conf-agent sets these)
	if v := os.Getenv("CORESWITCH_URL"); v != "" {
		conf.Http.Data = v + "/v3/batch"
		conf.Http.Alerts = v + "/v3/alerts"
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		conf.LogLevel = v
	}

	lvl, _ := logrus.ParseLevel(conf.LogLevel)
	log.SetLevel(lvl)

	// Load readings from SQLite (v2) or CSV
	var recs []*modbus.Reading

	sqlitePath := os.Getenv("SQLITE_PATH")
	readerID := os.Getenv("READER_ID")

	if sqlitePath != "" && readerID != "" {
		recs = loadFromSQLite(sqlitePath, readerID, conf)
	} else if conf.Modbus.ReadingsFile != "" {
		recs = loadFromCSV(conf.Modbus.ReadingsFile)
	}

	log.Infof("Config loaded: server=%s readings=%d", conf.Modbus.Server, len(recs))

	return conf, recs
}

// loadFromSQLite reads reader config + sensors from shared SQLite database.
func loadFromSQLite(dbPath, readerID string, conf *Config) []*modbus.Reading {

	dsn := "file:" + dbPath + "?mode=ro&_pragma=journal_mode%3DWAL&_pragma=busy_timeout%3D5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("Cannot open SQLite: %v", err)
	}
	defer db.Close()

	// Load reader config
	var name, protocol, configJSON string
	err = db.QueryRow(
		"SELECT name, protocol, config_json FROM readers WHERE id = ? AND status = 'active'",
		readerID,
	).Scan(&name, &protocol, &configJSON)
	if err != nil {
		log.Fatalf("Cannot load reader %s: %v", readerID, err)
	}

	var readerCfg map[string]any
	json.Unmarshal([]byte(configJSON), &readerCfg)

	// Set modbus config from SQLite
	if host, ok := readerCfg["host"].(string); ok {
		port := 502
		if p, ok := readerCfg["port"].(float64); ok {
			port = int(p)
		}
		conf.Modbus.Server = fmt.Sprintf("modbus-tcp://%s:%d", host, port)
	}
	if freq, ok := readerCfg["poll_interval_sec"].(float64); ok {
		conf.Modbus.FreqSec = int(freq)
	}
	if conf.Modbus.FreqSec == 0 {
		conf.Modbus.FreqSec = 5
	}
	if conf.Modbus.SingleReadCount == 0 {
		conf.Modbus.SingleReadCount = 100
	}
	if slaveID, ok := readerCfg["slave_id"].(float64); ok {
		conf.Modbus.SlaveID = int(slaveID)
	}

	// Load sensors → readings
	rows, err := db.Query(`
		SELECT id, name, config_json, tags_json, output, table_name
		FROM sensors WHERE reader_id = ? ORDER BY name`, readerID)
	if err != nil {
		log.Fatalf("Cannot query sensors: %v", err)
	}
	defer rows.Close()

	var recs []*modbus.Reading

	for rows.Next() {
		var sID, sName, sCfgJSON, sTagsJSON, sOutput, sTable string
		rows.Scan(&sID, &sName, &sCfgJSON, &sTagsJSON, &sOutput, &sTable)

		var sCfg map[string]any
		json.Unmarshal([]byte(sCfgJSON), &sCfg)

		var sTags map[string]any
		json.Unmarshal([]byte(sTagsJSON), &sTags)

		// Build tag string: key=val,key2=val2
		tagStr := formatTags(sTags)

		// Parse registers array
		regs, ok := sCfg["registers"].([]any)
		if !ok {
			continue
		}

		for _, r := range regs {
			rm, ok := r.(map[string]any)
			if !ok {
				continue
			}

			rec := &modbus.Reading{}
			rec.Equipment = sName
			rec.Table = sTable
			rec.Output = sOutput
			rec.Tags = tagStr

			if addr, ok := rm["address"].(float64); ok {
				rec.Addr = uint16(addr)
			}
			rec.RegType = getStr(rm, "register_type", "Holding")
			// Capitalize first letter to match production format
			if rec.RegType == "holding" {
				rec.RegType = "Holding"
			} else if rec.RegType == "input" {
				rec.RegType = "Input"
			}

			rec.Reading = getStr(rm, "field_key", "value")
			rec.DataType = getStr(rm, "data_type", "uint16")

			if scale, ok := rm["scale"].(float64); ok {
				rec.Scale = scale
			} else {
				rec.Scale = 1.0
			}

			recs = append(recs, rec)
		}
	}

	return recs
}

// loadFromCSV loads readings from CSV file (v1 compatibility).
func loadFromCSV(rfile string) []*modbus.Reading {

	fd, err := os.Open(rfile)
	if err != nil {
		log.Fatalf("Cannot open readings file %s - %s", rfile, err.Error())
	}
	defer fd.Close()

	scanner := bufio.NewScanner(fd)
	var recs []*modbus.Reading

	for scanner.Scan() {
		flds := strings.Split(scanner.Text(), ",")

		if len(flds) < 8 || flds[0][0] == '#' {
			continue
		}

		rec := &modbus.Reading{}
		rec.Equipment = flds[0]
		rec.Reading = flds[1]
		rec.RegType = flds[2]
		fmt.Sscan(flds[3], &rec.Addr)
		rec.DataType = flds[4]
		rec.Output = flds[5]
		rec.Table = flds[6]
		rec.Tags = strings.ReplaceAll(flds[7], "|", ",")
		rec.Scale = 1.0

		recs = append(recs, rec)
	}

	return recs
}

func formatTags(tags map[string]any) string {
	if len(tags) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tags))
	for k, v := range tags {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, ",")
}

func getStr(m map[string]any, key, fallback string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return fallback
}
