package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// GET /api/v1/users — list all users in the org (admin+)
func listUsersHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Use userID (not orgID) to look up org — avoids any context type issues
		// userID is always a string UUID that works correctly
		userID, _ := r.Context().Value(ctxUserID).(string)
		ctx := context.Background()

		rows, err := pool.Query(ctx,
			`SELECT u2.id::text, u2.email, u2.role, u2.created_at::text
			 FROM users u2
			 WHERE u2.org_id = (SELECT org_id FROM users WHERE id = $1::uuid)
			 ORDER BY u2.created_at`,
			userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer rows.Close()

		type userRow struct {
			ID        string `json:"id"`
			Email     string `json:"email"`
			Role      string `json:"role"`
			CreatedAt string `json:"created_at"`
		}
		users := []userRow{}
		for rows.Next() {
			var u userRow
			if err := rows.Scan(&u.ID, &u.Email, &u.Role, &u.CreatedAt); err != nil {
				continue // skip malformed rows
			}
			users = append(users, u)
		}
		writeJSON(w, http.StatusOK, users)
	}
}

// POST /api/v1/users — invite a new user to the org (admin+)
// Body: {"email":"...", "password":"...", "role":"viewer|editor|admin"}
func inviteUserHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		callerID, _ := r.Context().Value(ctxUserID).(string)
		orgID, _ := r.Context().Value(ctxOrgID).(string) // for response only
		ctx := context.Background()

		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Role     string `json:"role"` // viewer | editor | admin
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
			req.Email == "" {
			writeError(w, http.StatusBadRequest, "email is required")
			return
		}

		// Track whether a temp password was used
		usedTempPassword := req.Password == ""
		if req.Password == "" {
			req.Password = getEnvOrDefault("DEFAULT_USER_PASSWORD", "Qube@2024")
		}

		// Default role to viewer if not specified or invalid
		validRoles := map[string]bool{"viewer": true, "editor": true, "admin": true}
		if !validRoles[req.Role] {
			req.Role = "viewer"
		}

		var userID string
		err := pool.QueryRow(ctx,
			`INSERT INTO users (org_id, email, password_hash, role)
			 VALUES (
			   (SELECT org_id FROM users WHERE id = $1::uuid),
			   $2, crypt($3, gen_salt('bf',12)), $4
			 )
			 RETURNING id::text`,
			callerID,
			req.Email, req.Password, req.Role,
		).Scan(&userID)
		if err != nil {
			// Most likely a unique constraint on email
			writeError(w, http.StatusConflict, "email already exists in this or another org")
			return
		}

		resp := map[string]any{
			"user_id":          userID,
			"email":            req.Email,
			"role":             req.Role,
			"org_id":           orgID,
			"is_temp_password": usedTempPassword,
		}
		// Only return the password in the response if it was auto-generated
		// If the caller provided their own password, don't echo it back
		if usedTempPassword {
			resp["temp_password"] = req.Password
		}
		writeJSON(w, http.StatusCreated, resp)
	}
}

// PATCH /api/v1/users/:user_id — update role (admin only, cannot change own role)
// Body: {"role":"viewer|editor|admin"}
func updateUserRoleHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		callerID, _ := r.Context().Value(ctxUserID).(string)
		targetID := chi.URLParam(r, "user_id")
		ctx := context.Background()

		if callerID == targetID {
			writeError(w, http.StatusBadRequest, "cannot change your own role")
			return
		}

		var req struct {
			Role string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}

		validRoles := map[string]bool{"viewer": true, "editor": true, "admin": true}
		if !validRoles[req.Role] {
			writeError(w, http.StatusBadRequest, "role must be viewer, editor, or admin")
			return
		}

		result, err := pool.Exec(ctx,
			`UPDATE users SET role = $1
			 WHERE id::text = $2
			   AND org_id = (SELECT org_id FROM users WHERE id = $3::uuid)
			   AND role != 'superadmin'`,
			req.Role, targetID, callerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		if result.RowsAffected() == 0 {
			writeError(w, http.StatusNotFound, "user not found in your org")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"user_id": targetID,
			"role":    req.Role,
		})
	}
}

// DELETE /api/v1/users/:user_id — remove user from org (admin only)
func removeUserHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		callerID, _ := r.Context().Value(ctxUserID).(string)
		targetID := chi.URLParam(r, "user_id")
		ctx := context.Background()

		if callerID == targetID {
			writeError(w, http.StatusBadRequest, "cannot remove yourself")
			return
		}

		result, err := pool.Exec(ctx,
			`DELETE FROM users
			 WHERE id::text = $1
			   AND org_id = (SELECT org_id FROM users WHERE id = $2::uuid)
			   AND role != 'superadmin'`,
			targetID, callerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		if result.RowsAffected() == 0 {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"deleted": targetID})
	}
}

// GET /api/v1/users/me — get own profile
func getMeHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _ := r.Context().Value(ctxUserID).(string)
		orgID, _ := r.Context().Value(ctxOrgID).(string)
		ctx := context.Background()

		var email, role, orgName string
		err := pool.QueryRow(ctx,
			`SELECT u.email, u.role, o.name
			 FROM users u JOIN organisations o ON o.id = u.org_id
			 WHERE u.id::text = $1`,
			userID).Scan(&email, &role, &orgName)
		if err != nil {
			if err == pgx.ErrNoRows {
				writeError(w, http.StatusUnauthorized, "user not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"user_id":  userID,
			"org_id":   orgID,
			"email":    email,
			"role":     role,
			"org_name": orgName,
		})
	}
}
