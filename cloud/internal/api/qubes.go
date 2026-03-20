package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// --- LIST QUBES ---

func listQubesHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		rows, err := pool.Query(context.Background(),
			`SELECT q.id, q.last_seen, q.status, q.location_label,
			        q.claimed_at, q.config_version,
			        cs.hash
			 FROM qubes q
			 LEFT JOIN config_state cs ON cs.qube_id = q.id
			 WHERE q.org_id = $1
			 ORDER BY q.claimed_at DESC`, orgID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		result := make([]map[string]any, 0)
		for rows.Next() {
			var (
				id, status, location, configHash string
				lastSeen, claimedAt              *time.Time
				configVersion                    int
			)
			if err := rows.Scan(&id, &lastSeen, &status, &location,
				&claimedAt, &configVersion, &configHash); err != nil {
				continue
			}
			liveStatus := liveStatus(status, lastSeen)
			result = append(result, map[string]any{
				"id":             id,
				"status":         liveStatus,
				"location_label": location,
				"last_seen":      lastSeen,
				"claimed_at":     claimedAt,
				"config_version": configVersion,
				"config_hash":    configHash,
			})
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// --- GET QUBE ---

func getQubeHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		qubeID := chi.URLParam(r, "id")

		var (
			id, status, location, configHash string
			lastSeen, claimedAt              *time.Time
			configVersion                    int
			hashUpdated                      time.Time
		)
		err := pool.QueryRow(context.Background(),
			`SELECT q.id, q.last_seen, q.status, q.location_label,
			        q.claimed_at, q.config_version,
			        cs.hash, cs.generated_at
			 FROM qubes q
			 LEFT JOIN config_state cs ON cs.qube_id = q.id
			 WHERE q.id=$1 AND q.org_id=$2`, qubeID, orgID,
		).Scan(&id, &lastSeen, &status, &location,
			&claimedAt, &configVersion, &configHash, &hashUpdated)
		if err != nil {
			writeError(w, http.StatusNotFound, "qube not found")
			return
		}

		// Recent commands
		cmdRows, _ := pool.Query(context.Background(),
			`SELECT id, command, status, created_at FROM qube_commands
			 WHERE qube_id=$1 ORDER BY created_at DESC LIMIT 10`, qubeID)
		defer cmdRows.Close()
		cmds := make([]map[string]any, 0)
		for cmdRows.Next() {
			var cid, cmd, cstatus string
			var cat time.Time
			if err := cmdRows.Scan(&cid, &cmd, &cstatus, &cat); err == nil {
				cmds = append(cmds, map[string]any{
					"id": cid, "command": cmd,
					"status": cstatus, "created_at": cat,
				})
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"id":             id,
			"status":         liveStatus(status, lastSeen),
			"location_label": location,
			"last_seen":      lastSeen,
			"claimed_at":     claimedAt,
			"config_version": configVersion,
			"config_hash":    configHash,
			"hash_updated":   hashUpdated,
			"recent_commands": cmds,
		})
	}
}

// --- CLAIM QUBE ---

// claimQubeHandler — customer claims their device using the register_key
// printed on the device box / data sheet (generated at flash time by image-install.sh).
// This replaces the old "claim by qube_id" flow — customers never know the internal qube_id,
// they only know the register_key (e.g. "4D4L-R4KY-ZTQ5").
func claimQubeHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)

		var req struct {
			// Primary: claim by register_key (what customer has on device box)
			RegisterKey string `json:"register_key"`
			// Legacy / dev: still accept qube_id directly for testing
			QubeID      string `json:"qube_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "register_key required")
			return
		}

		ctx := context.Background()

		var qubeID string
		var currentOrg *string

		if req.RegisterKey != "" {
			// Production flow: look up by register_key
			err := pool.QueryRow(ctx,
				`SELECT id, org_id FROM qubes WHERE register_key=$1`, req.RegisterKey,
			).Scan(&qubeID, &currentOrg)
			if err != nil {
				writeError(w, http.StatusNotFound,
					"device not found — check the registration key on your device")
				return
			}
		} else if req.QubeID != "" {
			// Dev/test fallback: look up by qube_id directly
			err := pool.QueryRow(ctx,
				`SELECT id, org_id FROM qubes WHERE id=$1`, req.QubeID,
			).Scan(&qubeID, &currentOrg)
			if err != nil {
				writeError(w, http.StatusNotFound,
					fmt.Sprintf("qube %s not found in registry", req.QubeID))
				return
			}
		} else {
			writeError(w, http.StatusBadRequest, "register_key or qube_id required")
			return
		}

		if currentOrg != nil {
			writeError(w, http.StatusConflict, "device is already claimed by an organisation")
			return
		}

		// Get org secret to generate HMAC token
		var orgSecret string
		if err := pool.QueryRow(ctx,
			`SELECT org_secret FROM organisations WHERE id=$1`, orgID,
		).Scan(&orgSecret); err != nil {
			writeError(w, http.StatusInternalServerError, "org not found")
			return
		}

		// Generate the QUBE_TOKEN (HMAC of device_id + org_secret)
		// This is what the conf-agent uses to authenticate to TP-API
		authToken := computeHMAC(qubeID, orgSecret)

		_, err := pool.Exec(ctx,
			`UPDATE qubes
			 SET org_id=$1, auth_token_hash=$2, status='offline',
			     claimed_at=NOW(), config_version=1
			 WHERE id=$3`,
			orgID, authToken, qubeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "claim failed")
			return
		}

		// Initialise config state
		hash := computeConfigHash(qubeID, 1, "")
		pool.Exec(ctx,
			`UPDATE config_state SET hash=$1, generated_at=NOW() WHERE qube_id=$2`,
			hash, qubeID)

		writeJSON(w, http.StatusOK, map[string]any{
			"qube_id":      qubeID,
			"auth_token":   authToken,
			"message":      fmt.Sprintf("Device %s claimed. The device will self-configure within the next poll cycle.", qubeID),
			"next_step":    "The device conf-agent will automatically retrieve this token on its next poll. No manual action required.",
		})
	}
}

// --- UPDATE QUBE ---

func updateQubeHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		qubeID := chi.URLParam(r, "id")
		var req struct {
			LocationLabel string `json:"location_label"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
		ctx := context.Background()
		tag, err := pool.Exec(ctx,
			`UPDATE qubes SET location_label=$1, config_version=config_version+1
			 WHERE id=$2 AND org_id=$3`, req.LocationLabel, qubeID, orgID)
		if err != nil || tag.RowsAffected() == 0 {
			writeError(w, http.StatusNotFound, "qube not found")
			return
		}
		// Recalculate hash (any change triggers resync)
		var cv int
		_ = pool.QueryRow(ctx, `SELECT config_version FROM qubes WHERE id=$1`, qubeID).Scan(&cv)
		newHash := computeConfigHash(qubeID, cv, req.LocationLabel)
		_, _ = pool.Exec(ctx,
			`UPDATE config_state SET hash=$1, generated_at=NOW() WHERE qube_id=$2`, newHash, qubeID)
		writeJSON(w, http.StatusOK, map[string]any{
			"message":  "updated — Conf-Agent will pick up change within poll interval",
			"new_hash": newHash,
		})
	}
}

// --- HELPERS ---

func liveStatus(stored string, lastSeen *time.Time) string {
	if lastSeen == nil {
		return stored
	}
	if time.Since(*lastSeen) > 2*time.Minute {
		return "offline"
	}
	return "online"
}

func computeHMAC(qubeID, orgSecret string) string {
	mac := hmac.New(sha256.New, []byte(orgSecret))
	mac.Write([]byte(qubeID + ":" + orgSecret))
	return hex.EncodeToString(mac.Sum(nil))
}

func computeConfigHash(qubeID string, version int, location string) string {
	data := fmt.Sprintf("qube=%s:v=%d:loc=%s", qubeID, version, location)
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}
