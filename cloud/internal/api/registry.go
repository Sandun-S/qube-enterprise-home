package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RegistrySettings holds all image config resolved for the current mode
type RegistrySettings struct {
	Mode       string            `json:"mode"`
	GithubBase string            `json:"github_base"`
	GitlabBase string            `json:"gitlab_base"`
	Images     map[string]string `json:"images"`
	Resolved   map[string]string `json:"resolved"`
}

// GET /api/v1/admin/registry
func getRegistryHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings, err := loadRegistrySettings(context.Background(), pool)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load registry settings")
			return
		}
		writeJSON(w, http.StatusOK, settings)
	}
}

// PUT /api/v1/admin/registry
func updateRegistryHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var updates map[string]string
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body — send {key: value, ...}")
			return
		}

		ctx := context.Background()
		for key, value := range updates {
			_, err := pool.Exec(ctx,
				`INSERT INTO registry_config (key, value, updated_at)
				 VALUES ($1, $2, NOW())
				 ON CONFLICT (key) DO UPDATE SET value=$2, updated_at=NOW()`,
				key, value)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update: "+key)
				return
			}
		}

		settings, _ := loadRegistrySettings(ctx, pool)
		writeJSON(w, http.StatusOK, map[string]any{
			"updated":  len(updates),
			"settings": settings,
		})
	}
}

func loadRegistrySettings(ctx context.Context, pool *pgxpool.Pool) (*RegistrySettings, error) {
	rows, err := pool.Query(ctx, `SELECT key, value FROM registry_config`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cfg := map[string]string{}
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		cfg[k] = v
	}

	s := &RegistrySettings{
		Mode:       getOrDefault(cfg, "mode", "github"),
		GithubBase: getOrDefault(cfg, "github_base", "ghcr.io/sandun-s/qube-enterprise-home"),
		GitlabBase: getOrDefault(cfg, "gitlab_base", "registry.gitlab.com/iot-team4/product"),
		Images:     map[string]string{},
		Resolved:   map[string]string{},
	}

	for k, v := range cfg {
		if len(k) > 4 && k[:4] == "img_" {
			s.Images[k] = v
		}
	}

	s.Resolved = resolveImages(s)
	return s, nil
}

func resolveImages(s *RegistrySettings) map[string]string {
	resolved := map[string]string{}

	switch s.Mode {
	case "github":
		base := s.GithubBase
		resolved["conf_agent"]    = base + "/conf-agent:arm64.latest"
		resolved["influx_sql"]    = base + "/influx-to-sql:arm64.latest"
		resolved["modbus_reader"] = base + "/modbus-reader:arm64.latest"
		resolved["snmp_reader"]   = base + "/snmp-reader:arm64.latest"
		resolved["mqtt_reader"]   = base + "/mqtt-reader:arm64.latest"
		resolved["opcua_reader"]  = base + "/opcua-reader:arm64.latest"
		resolved["http_reader"]   = base + "/http-reader:arm64.latest"

	case "gitlab":
		resolved["conf_agent"]    = getOrDefault(s.Images, "img_conf_agent",
			s.GitlabBase+"/enterprise-conf-agent:arm64.latest")
		resolved["influx_sql"]    = getOrDefault(s.Images, "img_influx_sql",
			s.GitlabBase+"/enterprise-influx-to-sql:arm64.latest")
		resolved["modbus_reader"] = getOrDefault(s.Images, "img_modbus_reader",
			s.GitlabBase+"/modbus-reader:arm64.latest")
		resolved["snmp_reader"]   = getOrDefault(s.Images, "img_snmp_reader",
			s.GitlabBase+"/snmp-reader:arm64.latest")
		resolved["mqtt_reader"]   = getOrDefault(s.Images, "img_mqtt_reader",
			s.GitlabBase+"/mqtt-reader:arm64.latest")
		resolved["opcua_reader"]  = getOrDefault(s.Images, "img_opcua_reader",
			s.GitlabBase+"/opcua-reader:arm64.latest")
		resolved["http_reader"]   = getOrDefault(s.Images, "img_http_reader",
			s.GitlabBase+"/http-reader:arm64.latest")

	case "custom":
		resolved["conf_agent"]    = getOrDefault(s.Images, "img_conf_agent", "")
		resolved["influx_sql"]    = getOrDefault(s.Images, "img_influx_sql", "")
		resolved["modbus_reader"] = getOrDefault(s.Images, "img_modbus_reader", "")
		resolved["snmp_reader"]   = getOrDefault(s.Images, "img_snmp_reader", "")
		resolved["mqtt_reader"]   = getOrDefault(s.Images, "img_mqtt_reader", "")
		resolved["opcua_reader"]  = getOrDefault(s.Images, "img_opcua_reader", "")
		resolved["http_reader"]   = getOrDefault(s.Images, "img_http_reader", "")
	}

	return resolved
}

func getOrDefault(m map[string]string, key, def string) string {
	if v, ok := m[key]; ok && v != "" {
		return v
	}
	return def
}

// RegistryStatus is used for health/debug
type RegistryStatus struct {
	Mode      string            `json:"mode"`
	Images    map[string]string `json:"resolved_images"`
	UpdatedAt time.Time         `json:"updated_at"`
}
