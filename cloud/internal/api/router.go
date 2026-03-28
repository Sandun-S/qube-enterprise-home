package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewRouter(pool, telemetryPool *pgxpool.Pool, jwtSecret string) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	// Create WebSocket hubs (shared across all handlers)
	hub := NewWSHub()
	globalHub = hub
	globalPool = pool
	hub.OnConnect = func(qubeID string) {
		pool.Exec(context.Background(),
			`UPDATE qubes SET ws_connected=TRUE, last_seen=NOW(), status='online' WHERE id=$1`, qubeID)
	}
	hub.OnDisconnect = func(qubeID string) {
		pool.Exec(context.Background(),
			`UPDATE qubes SET ws_connected=FALSE WHERE id=$1`, qubeID)
	}

	dashHub := NewDashboardHub()
	globalDashHub = dashHub

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"status":               "ok",
			"service":              "cloud-api",
			"version":              "2",
			"ws_connections":       len(hub.ConnectedQubes()),
			"dashboard_connections": dashHub.ConnectedCount(),
		})
	})

	// ── Public ──────────────────────────────────────────────────────────────
	r.Post("/api/v1/auth/register", registerHandler(pool, jwtSecret))
	r.Post("/api/v1/auth/login", loginHandler(pool, jwtSecret))

	// ── WebSocket endpoints ─────────────────────────────────────────────────
	r.Get("/ws", wsHandler(pool, hub))
	r.Get("/ws/dashboard", dashWSHandler(pool, dashHub, jwtSecret))

	// ── Protected (JWT required) ─────────────────────────────────────────────
	r.Group(func(r chi.Router) {
		r.Use(jwtMiddleware(jwtSecret))

		// ── Qubes ──
		r.Get("/api/v1/qubes", listQubesHandler(pool))
		r.Get("/api/v1/qubes/{id}", getQubeHandler(pool))
		r.Get("/api/v1/qubes/{id}/sensors", listAllSensorsForQubeHandler(pool))
		r.Get("/api/v1/qubes/{id}/readers", listReadersHandler(pool))
		r.Get("/api/v1/qubes/{id}/containers", listContainersHandler(pool))

		// ── Readers ──
		r.Get("/api/v1/readers/{reader_id}", getReaderHandler(pool))
		r.Get("/api/v1/readers/{reader_id}/sensors", listSensorsHandler(pool))

		// ── Device Templates ──
		r.Get("/api/v1/device-templates", listDeviceTemplatesHandler(pool))
		r.Get("/api/v1/device-templates/{id}", getDeviceTemplateHandler(pool))

		// ── Reader Templates ──
		r.Get("/api/v1/reader-templates", listReaderTemplatesHandler(pool))
		r.Get("/api/v1/reader-templates/{id}", getReaderTemplateHandler(pool))

		// ── Protocols ──
		r.Get("/api/v1/protocols", listProtocolsHandler(pool))

		// ── Telemetry (queries go to telemetry database) ──
		r.Get("/api/v1/data/readings", readingsHandler(pool, telemetryPool))
		r.Get("/api/v1/data/sensors/{id}/latest", latestReadingHandler(pool, telemetryPool))

		// ── Commands ──
		r.Get("/api/v1/commands/{id}", getCommandHandler(pool))

		// ── Profile ──
		r.Get("/api/v1/users/me", getMeHandler(pool))

		// ── Editor+ (editor, admin, superadmin) ──────────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(requireRole("admin", "editor", "superadmin"))

			// Qubes — update, commands
			r.Put("/api/v1/qubes/{id}", updateQubeHandler(pool))
			r.Post("/api/v1/qubes/{id}/commands", sendCommandHandler(pool, hub))

			// Readers — CRUD
			r.Post("/api/v1/qubes/{id}/readers", createReaderHandler(pool))
			r.Put("/api/v1/readers/{reader_id}", updateReaderHandler(pool))
			r.Delete("/api/v1/readers/{reader_id}", deleteReaderHandler(pool))

			// Sensors — CRUD
			r.Post("/api/v1/readers/{reader_id}/sensors", createSensorHandler(pool))
			r.Put("/api/v1/sensors/{sensor_id}", updateSensorHandler(pool))
			r.Delete("/api/v1/sensors/{sensor_id}", deleteSensorHandler(pool))

			// Device Templates — CRUD
			r.Post("/api/v1/device-templates", createDeviceTemplateHandler(pool))
			r.Put("/api/v1/device-templates/{id}", updateDeviceTemplateHandler(pool))
			r.Delete("/api/v1/device-templates/{id}", deleteDeviceTemplateHandler(pool))
			r.Patch("/api/v1/device-templates/{id}/config", patchDeviceTemplateConfigHandler(pool))
		})

		// ── Admin+ ───────────────────────────────────────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(requireRole("admin", "superadmin"))
			r.Get("/api/v1/users", listUsersHandler(pool))
			r.Post("/api/v1/users", inviteUserHandler(pool))
			r.Patch("/api/v1/users/{user_id}", updateUserRoleHandler(pool))
			r.Delete("/api/v1/users/{user_id}", removeUserHandler(pool))
			r.Post("/api/v1/qubes/claim", claimQubeHandler(pool))
		})

		// ── Superadmin ───────────────────────────────────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(requireRole("superadmin"))
			r.Get("/api/v1/admin/registry", getRegistryHandler(pool))
			r.Put("/api/v1/admin/registry", updateRegistryHandler(pool))

			// Reader Templates — managed by IoT team
			r.Post("/api/v1/reader-templates", createReaderTemplateHandler(pool))
			r.Put("/api/v1/reader-templates/{id}", updateReaderTemplateHandler(pool))
			r.Delete("/api/v1/reader-templates/{id}", deleteReaderTemplateHandler(pool))
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
