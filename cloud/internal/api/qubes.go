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
			        q.ws_connected, q.poll_interval_sec,
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
				configVersion, pollInterval      int
				wsConnected                      bool
			)
			if err := rows.Scan(&id, &lastSeen, &status, &location,
				&claimedAt, &configVersion,
				&wsConnected, &pollInterval,
				&configHash); err != nil {
				continue
			}
			result = append(result, map[string]any{
				"id":               id,
				"status":           liveStatus(status, lastSeen),
				"location_label":   location,
				"last_seen":        lastSeen,
				"claimed_at":       claimedAt,
				"config_version":   configVersion,
				"config_hash":      configHash,
				"ws_connected":     wsConnected,
				"poll_interval_sec": pollInterval,
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
			configVersion, pollInterval      int
			wsConnected                      bool
			hashUpdated                      time.Time
			capabilitiesRaw                  []byte
		)
		err := pool.QueryRow(context.Background(),
			`SELECT q.id, q.last_seen, q.status, q.location_label,
			        q.claimed_at, q.config_version,
			        q.ws_connected, q.poll_interval_sec, q.capabilities,
			        cs.hash, cs.generated_at
			 FROM qubes q
			 LEFT JOIN config_state cs ON cs.qube_id = q.id
			 WHERE q.id=$1 AND q.org_id=$2`, qubeID, orgID,
		).Scan(&id, &lastSeen, &status, &location,
			&claimedAt, &configVersion,
			&wsConnected, &pollInterval, &capabilitiesRaw,
			&configHash, &hashUpdated)
		if err != nil {
			writeError(w, http.StatusNotFound, "qube not found")
			return
		}

		var capabilities any
		json.Unmarshal(capabilitiesRaw, &capabilities)

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
			"id":               id,
			"status":           liveStatus(status, lastSeen),
			"location_label":   location,
			"last_seen":        lastSeen,
			"claimed_at":       claimedAt,
			"config_version":   configVersion,
			"config_hash":      configHash,
			"hash_updated":     hashUpdated,
			"ws_connected":     wsConnected,
			"poll_interval_sec": pollInterval,
			"capabilities":     capabilities,
			"recent_commands":  cmds,
		})
	}
}

// --- LIST ALL QUBES (superadmin) ---

func listAllQubesAdminHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := pool.Query(context.Background(),
			`SELECT q.id, q.last_seen, q.status, q.location_label,
			        q.claimed_at, q.ws_connected,
			        o.id AS org_id, o.name AS org_name
			 FROM qubes q
			 LEFT JOIN organisations o ON o.id = q.org_id
			 WHERE q.org_id IS NOT NULL
			 ORDER BY q.claimed_at DESC`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		result := make([]map[string]any, 0)
		for rows.Next() {
			var (
				id, status, location string
				orgID, orgName       string
				lastSeen, claimedAt  *time.Time
				wsConnected          bool
			)
			if err := rows.Scan(&id, &lastSeen, &status, &location,
				&claimedAt, &wsConnected, &orgID, &orgName); err != nil {
				continue
			}
			result = append(result, map[string]any{
				"id":             id,
				"status":         liveStatus(status, lastSeen),
				"location_label": location,
				"last_seen":      lastSeen,
				"claimed_at":     claimedAt,
				"ws_connected":   wsConnected,
				"org_id":         orgID,
				"org_name":       orgName,
			})
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// --- CLAIM QUBE ---

func claimQubeHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)

		var req struct {
			RegisterKey string `json:"register_key"`
			QubeID      string `json:"qube_id"` // legacy/dev fallback
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "register_key required")
			return
		}

		ctx := context.Background()

		var qubeID string
		var currentOrg *string

		if req.RegisterKey != "" {
			err := pool.QueryRow(ctx,
				`SELECT id, org_id FROM qubes WHERE register_key=$1`, req.RegisterKey,
			).Scan(&qubeID, &currentOrg)
			if err != nil {
				writeError(w, http.StatusNotFound,
					"device not found — check the registration key on your device")
				return
			}
		} else if req.QubeID != "" {
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

		var orgSecret string
		if err := pool.QueryRow(ctx,
			`SELECT org_secret FROM organisations WHERE id=$1`, orgID,
		).Scan(&orgSecret); err != nil {
			writeError(w, http.StatusInternalServerError, "org not found")
			return
		}

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
		pool.Exec(ctx,
			`UPDATE config_state SET hash='', config_version=1, generated_at=NOW() WHERE qube_id=$1`,
			qubeID)

		writeJSON(w, http.StatusOK, map[string]any{
			"qube_id":    qubeID,
			"auth_token": authToken,
			"message":    fmt.Sprintf("Device %s claimed. The device will self-configure within the next sync cycle.", qubeID),
		})
	}
}

// --- UPDATE QUBE ---

func updateQubeHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		qubeID := chi.URLParam(r, "id")
		var req struct {
			LocationLabel   *string `json:"location_label"`
			PollIntervalSec *int    `json:"poll_interval_sec"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
		ctx := context.Background()

		if req.LocationLabel != nil {
			pool.Exec(ctx,
				`UPDATE qubes SET location_label=$1 WHERE id=$2 AND org_id=$3`,
				*req.LocationLabel, qubeID, orgID)
		}
		if req.PollIntervalSec != nil {
			pool.Exec(ctx,
				`UPDATE qubes SET poll_interval_sec=$1 WHERE id=$2 AND org_id=$3`,
				*req.PollIntervalSec, qubeID, orgID)
		}

		hash, _ := recomputeConfigHash(ctx, pool, qubeID)
		writeJSON(w, http.StatusOK, map[string]any{
			"message":  "updated — conf-agent will sync",
			"new_hash": hash,
		})
	}
}

// --- UNCLAIM QUBE ---

func unclaimQubeHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		qubeID := chi.URLParam(r, "id")
		ctx := context.Background()

		// Verify the qube exists and is currently claimed
		var orgID *string
		err := pool.QueryRow(ctx,
			`SELECT org_id FROM qubes WHERE id=$1`, qubeID,
		).Scan(&orgID)
		if err != nil {
			writeError(w, http.StatusNotFound, "qube not found")
			return
		}
		if orgID == nil {
			writeError(w, http.StatusConflict, "qube is not claimed by any organisation")
			return
		}

		// Cascade delete config data (sensors → readers → containers → commands)
		pool.Exec(ctx,
			`DELETE FROM sensors WHERE reader_id IN (SELECT id FROM readers WHERE qube_id=$1)`, qubeID)
		pool.Exec(ctx, `DELETE FROM readers WHERE qube_id=$1`, qubeID)
		pool.Exec(ctx, `DELETE FROM containers WHERE qube_id=$1`, qubeID)
		pool.Exec(ctx, `DELETE FROM qube_commands WHERE qube_id=$1`, qubeID)

		// Reset config state
		pool.Exec(ctx,
			`UPDATE config_state SET hash='', config_version=0, generated_at=NOW() WHERE qube_id=$1`, qubeID)

		// Unclaim the qube — returns it to the unclaimed pool
		_, err = pool.Exec(ctx,
			`UPDATE qubes
			 SET org_id=NULL, auth_token_hash=NULL, status='unclaimed',
			     claimed_at=NULL, ws_connected=FALSE, config_version=0
			 WHERE id=$1`, qubeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "unclaim failed")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"qube_id": qubeID,
			"message": fmt.Sprintf("Device %s has been unclaimed and is available for re-claiming.", qubeID),
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
