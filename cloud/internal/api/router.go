package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewRouter(pool *pgxpool.Pool, jwtSecret string) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "service": "cloud-api"})
	})

	// ── Public ──────────────────────────────────────────────────────────────
	r.Post("/api/v1/auth/register", registerHandler(pool, jwtSecret))
	r.Post("/api/v1/auth/login", loginHandler(pool, jwtSecret))

	// ── Protected (JWT required) ─────────────────────────────────────────────
	r.Group(func(r chi.Router) {
		r.Use(jwtMiddleware(jwtSecret))

		// Qubes
		r.Get("/api/v1/qubes", listQubesHandler(pool))
		r.Get("/api/v1/qubes/{id}", getQubeHandler(pool))
		r.Put("/api/v1/qubes/{id}", updateQubeHandler(pool))
		r.Post("/api/v1/qubes/{id}/commands", sendCommandHandler(pool))
		r.Get("/api/v1/commands/{id}", getCommandHandler(pool))
		r.Get("/api/v1/qubes/{id}/sensors", listAllSensorsForQubeHandler(pool))
		r.Get("/api/v1/qubes/{id}/gateways", listGatewaysHandler(pool))

		// Gateways
		r.Post("/api/v1/qubes/{id}/gateways", createGatewayHandler(pool))
		r.Delete("/api/v1/gateways/{gateway_id}", deleteGatewayHandler(pool))

		// Sensors
		r.Get("/api/v1/gateways/{gateway_id}/sensors", listSensorsHandler(pool))
		r.Post("/api/v1/gateways/{gateway_id}/sensors", createSensorHandler(pool))
		r.Delete("/api/v1/sensors/{sensor_id}", deleteSensorHandler(pool))

		// Sensor register rows — view and fix individual CSV rows
		r.Get("/api/v1/sensors/{sensor_id}/rows", listSensorRowsHandler(pool))
		r.Post("/api/v1/sensors/{sensor_id}/rows", addSensorRowHandler(pool))
		r.Put("/api/v1/sensors/{sensor_id}/rows/{row_id}", updateSensorRowHandler(pool))
		r.Delete("/api/v1/sensors/{sensor_id}/rows/{row_id}", deleteSensorRowHandler(pool))

		// Templates — catalog browsing (all authenticated users)
		r.Get("/api/v1/templates", listTemplatesHandler(pool))
		r.Get("/api/v1/templates/{id}", getTemplateHandler(pool))
		r.Get("/api/v1/templates/{id}/preview", previewTemplateHandler(pool))

		// Templates — org-scoped create/update/delete (admin + editor)
		r.Group(func(r chi.Router) {
			r.Use(requireRole("admin", "editor", "superadmin"))
			r.Post("/api/v1/templates", createTemplateHandler(pool))
			r.Put("/api/v1/templates/{id}", updateTemplateHandler(pool))
			r.Delete("/api/v1/templates/{id}", deleteTemplateHandler(pool))
			// Patch individual registers — IoT team internal use
			r.Patch("/api/v1/templates/{id}/registers", patchTemplateRegistersHandler(pool))
		})

		// Telemetry
		r.Get("/api/v1/data/readings", readingsHandler(pool))
		r.Get("/api/v1/data/sensors/{id}/latest", latestReadingHandler(pool))

		// Admin only
		r.Group(func(r chi.Router) {
			r.Use(requireRole("admin", "superadmin"))
			r.Post("/api/v1/qubes/claim", claimQubeHandler(pool))
		})

		// Superadmin only — registry configuration
		r.Group(func(r chi.Router) {
			r.Use(requireRole("superadmin"))
			r.Get("/api/v1/admin/registry", getRegistryHandler(pool))
			r.Put("/api/v1/admin/registry", updateRegistryHandler(pool))
		})
	})

	return r
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
