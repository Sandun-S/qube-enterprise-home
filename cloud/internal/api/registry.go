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
	Mode        string            `json:"mode"`         // github | gitlab | custom
	GithubBase  string            `json:"github_base"`
	GitlabBase  string            `json:"gitlab_base"`
	Images      map[string]string `json:"images"`       // key → full image path
	Resolved    map[string]string `json:"resolved"`     // short_name → resolved full image
}

// GET /api/v1/admin/registry — get current registry settings
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

// PUT /api/v1/admin/registry — update registry settings (superadmin only)
// Body: {"mode":"github"} or {"mode":"gitlab"} or {"mode":"custom","img_conf_agent":"..."}
// Can update individual keys or switch mode entirely.
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

// loadRegistrySettings reads registry_config table and resolves image names
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

	// Copy per-image entries
	for k, v := range cfg {
		if len(k) > 4 && k[:4] == "img_" {
			s.Images[k] = v
		}
	}

	// Resolve final image paths based on mode
	s.Resolved = resolveImages(s)
	return s, nil
}

// resolveImages returns the final Docker image path for each service
// based on mode and per-image overrides
func resolveImages(s *RegistrySettings) map[string]string {
	resolved := map[string]string{}

	switch s.Mode {
	case "github":
		// GitHub single-repo: all images under the same base path
		// short name matches what was built in CI (conf-agent, influx-to-sql, mqtt-gateway)
		base := s.GithubBase
		resolved["conf_agent"]  = base + "/conf-agent:arm64.latest"
		resolved["influx_sql"]  = base + "/influx-to-sql:arm64.latest"
		resolved["mqtt_gw"]     = base + "/mqtt-gateway:arm64.latest"
		resolved["modbus"]      = base + "/modbus-gateway:arm64.latest"
		resolved["opcua"]       = base + "/opc-ua-gateway:arm64.latest"
		resolved["snmp"]        = base + "/snmp-gateway:arm64.latest"

	case "gitlab":
		// GitLab separate repos: use per-image settings (which have enterprise- prefix)
		resolved["conf_agent"]  = getOrDefault(s.Images, "img_conf_agent",
			s.GitlabBase+"/enterprise-conf-agent:arm64.latest")
		resolved["influx_sql"]  = getOrDefault(s.Images, "img_influx_sql",
			s.GitlabBase+"/enterprise-influx-to-sql:arm64.latest")
		resolved["mqtt_gw"]     = getOrDefault(s.Images, "img_mqtt_gw",
			s.GitlabBase+"/mqtt-gateway:arm64.latest")
		resolved["modbus"]      = getOrDefault(s.Images, "img_modbus",
			s.GitlabBase+"/modbus-gateway:arm64.latest")
		resolved["opcua"]       = getOrDefault(s.Images, "img_opcua",
			s.GitlabBase+"/opc-ua-gateway:arm64.latest")
		resolved["snmp"]        = getOrDefault(s.Images, "img_snmp",
			s.GitlabBase+"/snmp-gateway:arm64.latest")

	case "custom":
		// Custom: every image is individually specified in img_* keys
		resolved["conf_agent"]  = getOrDefault(s.Images, "img_conf_agent", "")
		resolved["influx_sql"]  = getOrDefault(s.Images, "img_influx_sql", "")
		resolved["mqtt_gw"]     = getOrDefault(s.Images, "img_mqtt_gw", "")
		resolved["modbus"]      = getOrDefault(s.Images, "img_modbus", "")
		resolved["opcua"]       = getOrDefault(s.Images, "img_opcua", "")
		resolved["snmp"]        = getOrDefault(s.Images, "img_snmp", "")
	}

	return resolved
}

func getOrDefault(m map[string]string, key, def string) string {
	if v, ok := m[key]; ok && v != "" {
		return v
	}
	return def
}

// ImageForProtocol returns the Docker image for a gateway protocol
// using settings from the database
func ImageForProtocol(pool *pgxpool.Pool, protocol string) string {
	settings, err := loadRegistrySettings(context.Background(), pool)
	if err != nil {
		return "busybox:latest"
	}
	switch protocol {
	case "modbus_tcp": return settings.Resolved["modbus"]
	case "opcua":      return settings.Resolved["opcua"]
	case "snmp":       return settings.Resolved["snmp"]
	case "mqtt":       return settings.Resolved["mqtt_gw"]
	default:           return "busybox:latest"
	}
}

// ImageForService returns the Docker image for an Enterprise service
func ImageForService(pool *pgxpool.Pool, serviceKey string) string {
	settings, err := loadRegistrySettings(context.Background(), pool)
	if err != nil {
		return "busybox:latest"
	}
	return settings.Resolved[serviceKey]
}

// RegistryStatus is used for health/debug endpoint
type RegistryStatus struct {
	Mode     string            `json:"mode"`
	Images   map[string]string `json:"resolved_images"`
	UpdatedAt time.Time        `json:"updated_at"`
}
