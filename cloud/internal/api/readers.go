package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GET /api/v1/qubes/:id/readers
func listReadersHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		qubeID := chi.URLParam(r, "id")

		var count int
		pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM qubes WHERE id=$1 AND org_id=$2`, qubeID, orgID).Scan(&count)
		if count == 0 {
			writeError(w, http.StatusNotFound, "qube not found")
			return
		}

		rows, err := pool.Query(context.Background(),
			`SELECT rd.id, rd.name, rd.protocol, rd.config_json,
			        rd.status, rd.version, rd.created_at, rd.updated_at,
			        COUNT(s.id) AS sensor_count
			 FROM readers rd
			 LEFT JOIN sensors s ON s.reader_id = rd.id AND s.status='active'
			 WHERE rd.qube_id=$1
			 GROUP BY rd.id ORDER BY rd.created_at ASC`, qubeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		result := make([]map[string]any, 0)
		for rows.Next() {
			var id, name, protocol, status string
			var version, sensorCount int
			var cfgRaw []byte
			var createdAt, updatedAt time.Time
			if err := rows.Scan(&id, &name, &protocol, &cfgRaw,
				&status, &version, &createdAt, &updatedAt, &sensorCount); err != nil {
				continue
			}
			var cfg any
			json.Unmarshal(cfgRaw, &cfg)
			result = append(result, map[string]any{
				"id": id, "name": name, "protocol": protocol,
				"config_json": cfg, "status": status, "version": version,
				"sensor_count": sensorCount,
				"created_at": createdAt, "updated_at": updatedAt,
			})
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// GET /api/v1/readers/:reader_id
func getReaderHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		readerID := chi.URLParam(r, "reader_id")

		var id, name, protocol, status string
		var version int
		var cfgRaw []byte
		var createdAt, updatedAt time.Time
		err := pool.QueryRow(context.Background(),
			`SELECT rd.id, rd.name, rd.protocol, rd.config_json,
			        rd.status, rd.version, rd.created_at, rd.updated_at
			 FROM readers rd JOIN qubes q ON q.id=rd.qube_id
			 WHERE rd.id=$1 AND q.org_id=$2`, readerID, orgID,
		).Scan(&id, &name, &protocol, &cfgRaw, &status, &version, &createdAt, &updatedAt)
		if err != nil {
			writeError(w, http.StatusNotFound, "reader not found")
			return
		}
		var cfg any
		json.Unmarshal(cfgRaw, &cfg)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": id, "name": name, "protocol": protocol,
			"config_json": cfg, "status": status, "version": version,
			"created_at": createdAt, "updated_at": updatedAt,
		})
	}
}

// POST /api/v1/qubes/:id/readers
func createReaderHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		qubeID := chi.URLParam(r, "id")

		var count int
		pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM qubes WHERE id=$1 AND org_id=$2`, qubeID, orgID).Scan(&count)
		if count == 0 {
			writeError(w, http.StatusNotFound, "qube not found")
			return
		}

		var req struct {
			Name       string `json:"name"`
			Protocol   string `json:"protocol"`
			TemplateID string `json:"template_id"` // reader_template UUID
			ConfigJSON any    `json:"config_json"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
			req.Name == "" || req.Protocol == "" {
			writeError(w, http.StatusBadRequest, "name and protocol are required")
			return
		}

		// Validate protocol
		var protoExists bool
		pool.QueryRow(context.Background(),
			`SELECT EXISTS(SELECT 1 FROM protocols WHERE id=$1 AND is_active=TRUE)`,
			req.Protocol).Scan(&protoExists)
		if !protoExists {
			writeError(w, http.StatusBadRequest, "unknown or inactive protocol: "+req.Protocol)
			return
		}

		cfgBytes, _ := json.Marshal(req.ConfigJSON)
		if cfgBytes == nil {
			cfgBytes = []byte("{}")
		}

		// Resolve image from reader template
		var imageSuffix string
		if req.TemplateID != "" {
			pool.QueryRow(context.Background(),
				`SELECT image_suffix FROM reader_templates WHERE id=$1 AND protocol=$2`,
				req.TemplateID, req.Protocol).Scan(&imageSuffix)
		}
		if imageSuffix == "" {
			// Fallback: look up default reader template for this protocol
			pool.QueryRow(context.Background(),
				`SELECT image_suffix FROM reader_templates WHERE protocol=$1 LIMIT 1`,
				req.Protocol).Scan(&imageSuffix)
		}

		ctx := context.Background()
		tx, err := pool.Begin(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer tx.Rollback(ctx)

		// Create reader
		var readerID string
		templateIDArg := any(nil)
		if req.TemplateID != "" {
			templateIDArg = req.TemplateID
		}
		if err := tx.QueryRow(ctx,
			`INSERT INTO readers (qube_id, name, protocol, template_id, config_json)
			 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			qubeID, req.Name, req.Protocol, templateIDArg, cfgBytes,
		).Scan(&readerID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create reader: "+err.Error())
			return
		}

		// Auto-create container for this reader
		serviceName := sanitizeServiceName(req.Name)
		image := resolveReaderImage(pool, req.Protocol, imageSuffix)
		envJSON, _ := json.Marshal(map[string]any{
			"READER_ID":      readerID,
			"SQLITE_PATH":    "/opt/qube/data/qube.db",
			"CORESWITCH_URL": "http://core-switch:8585",
			"LOG_LEVEL":      "info",
		})

		var containerID string
		if err := tx.QueryRow(ctx,
			`INSERT INTO containers (qube_id, reader_id, name, image, env_json)
			 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			qubeID, readerID, serviceName, image, envJSON,
		).Scan(&containerID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create container")
			return
		}

		if err := tx.Commit(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "commit failed")
			return
		}

		hash, _ := recomputeConfigHash(ctx, pool, qubeID)
		writeJSON(w, http.StatusCreated, map[string]any{
			"reader_id":    readerID,
			"container_id": containerID,
			"service_name": serviceName,
			"image":        image,
			"new_hash":     hash,
			"message":      "Reader created. Conf-Agent will deploy within the next sync.",
		})
	}
}

// PUT /api/v1/readers/:reader_id
func updateReaderHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		readerID := chi.URLParam(r, "reader_id")

		var qubeID string
		err := pool.QueryRow(context.Background(),
			`SELECT rd.qube_id FROM readers rd JOIN qubes q ON q.id=rd.qube_id
			 WHERE rd.id=$1 AND q.org_id=$2`, readerID, orgID).Scan(&qubeID)
		if err != nil {
			writeError(w, http.StatusNotFound, "reader not found")
			return
		}

		var req struct {
			Name       *string `json:"name"`
			ConfigJSON any     `json:"config_json"`
			Status     *string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}

		setParts := []string{}
		args := []any{}
		i := 1
		if req.Name != nil {
			setParts = append(setParts, fmt.Sprintf("name=$%d", i))
			args = append(args, *req.Name)
			i++
		}
		if req.ConfigJSON != nil {
			b, _ := json.Marshal(req.ConfigJSON)
			setParts = append(setParts, fmt.Sprintf("config_json=$%d", i))
			args = append(args, b)
			i++
		}
		if req.Status != nil && (*req.Status == "active" || *req.Status == "disabled") {
			setParts = append(setParts, fmt.Sprintf("status=$%d", i))
			args = append(args, *req.Status)
			i++
		}
		if len(setParts) == 0 {
			writeError(w, http.StatusBadRequest, "nothing to update")
			return
		}

		setParts = append(setParts, fmt.Sprintf("version=version+1, updated_at=NOW()"))
		query := fmt.Sprintf("UPDATE readers SET %s WHERE id=$%d",
			strings.Join(setParts, ", "), i)
		args = append(args, readerID)
		if _, err := pool.Exec(context.Background(), query, args...); err != nil {
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}

		hash, _ := recomputeConfigHash(context.Background(), pool, qubeID)
		writeJSON(w, http.StatusOK, map[string]any{
			"message":  "reader updated — conf-agent will sync",
			"new_hash": hash,
		})
	}
}

// DELETE /api/v1/readers/:reader_id
func deleteReaderHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		readerID := chi.URLParam(r, "reader_id")

		var qubeID string
		err := pool.QueryRow(context.Background(),
			`SELECT rd.qube_id FROM readers rd JOIN qubes q ON q.id=rd.qube_id
			 WHERE rd.id=$1 AND q.org_id=$2`, readerID, orgID).Scan(&qubeID)
		if err != nil {
			writeError(w, http.StatusNotFound, "reader not found")
			return
		}

		ctx := context.Background()
		if _, err := pool.Exec(ctx, `DELETE FROM readers WHERE id=$1`, readerID); err != nil {
			writeError(w, http.StatusInternalServerError, "delete failed")
			return
		}

		hash, _ := recomputeConfigHash(ctx, pool, qubeID)
		writeJSON(w, http.StatusOK, map[string]any{
			"deleted":  true,
			"new_hash": hash,
			"message":  "Reader deleted. Container will be removed on next sync.",
		})
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func sanitizeServiceName(name string) string {
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			out = append(out, c)
		} else if c >= 'A' && c <= 'Z' {
			out = append(out, c+32)
		} else {
			out = append(out, '-')
		}
	}
	result := string(out)
	for len(result) > 0 && result[0] == '-' {
		result = result[1:]
	}
	for len(result) > 0 && result[len(result)-1] == '-' {
		result = result[:len(result)-1]
	}
	return result
}

func resolveReaderImage(pool *pgxpool.Pool, protocol, imageSuffix string) string {
	settings, err := loadRegistrySettings(context.Background(), pool)
	if err != nil || imageSuffix == "" {
		return "busybox:latest"
	}
	switch settings.Mode {
	case "github":
		return settings.GithubBase + "/" + imageSuffix + ":arm64.latest"
	case "gitlab":
		imgKey := "img_" + strings.ReplaceAll(imageSuffix, "-", "_")
		if v, ok := settings.Images[imgKey]; ok && v != "" {
			return v
		}
		return settings.GitlabBase + "/" + imageSuffix + ":arm64.latest"
	default:
		imgKey := "img_" + strings.ReplaceAll(imageSuffix, "-", "_")
		if v, ok := settings.Images[imgKey]; ok && v != "" {
			return v
		}
		return imageSuffix + ":latest"
	}
}
