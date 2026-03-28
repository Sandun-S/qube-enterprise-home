package tpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ctxKey string

const (
	ctxQubeID ctxKey = "qube_id"
	ctxOrgID  ctxKey = "tp_org_id"
)

func NewRouter(pool, telemetryPool *pgxpool.Pool) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "service": "tp-api", "version": "2"})
	})

	// Public — device self-registration (no auth, uses register_key)
	r.Post("/v1/device/register", deviceRegisterHandler(pool))

	r.Group(func(r chi.Router) {
		r.Use(qubeAuthMiddleware(pool))

		// Sync — config state + SQLite data download
		r.Get("/v1/sync/state", syncStateHandler(pool))
		r.Get("/v1/sync/config", syncConfigHandler(pool))

		// Heartbeat
		r.Post("/v1/heartbeat", heartbeatHandler(pool))

		// Commands
		r.Post("/v1/commands/poll", pollCommandsHandler(pool))
		r.Post("/v1/commands/{id}/ack", ackCommandHandler(pool))

		// Telemetry — writes to telemetry database (qubedata)
		r.Post("/v1/telemetry/ingest", telemetryIngestHandler(telemetryPool))
	})

	return r
}

func qubeAuthMiddleware(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			qubeID := r.Header.Get("X-Qube-ID")
			auth := r.Header.Get("Authorization")
			if qubeID == "" || !strings.HasPrefix(auth, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "X-Qube-ID and Authorization headers required")
				return
			}
			token := strings.TrimPrefix(auth, "Bearer ")

			var orgSecret, orgID string
			err := pool.QueryRow(context.Background(),
				`SELECT o.org_secret, o.id
				 FROM qubes q JOIN organisations o ON o.id = q.org_id
				 WHERE q.id=$1 AND q.org_id IS NOT NULL`, qubeID,
			).Scan(&orgSecret, &orgID)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "qube not claimed or not registered")
				return
			}

			expected := computeHMAC(qubeID, orgSecret)
			if !hmac.Equal([]byte(expected), []byte(token)) {
				writeError(w, http.StatusUnauthorized, "invalid qube token")
				return
			}

			ctx := context.WithValue(r.Context(), ctxQubeID, qubeID)
			ctx = context.WithValue(ctx, ctxOrgID, orgID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func computeHMAC(qubeID, orgSecret string) string {
	mac := hmac.New(sha256.New, []byte(orgSecret))
	mac.Write([]byte(qubeID + ":" + orgSecret))
	return hex.EncodeToString(mac.Sum(nil))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
