package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func registerHandler(pool *pgxpool.Pool, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OrgName  string `json:"org_name"`
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
			req.OrgName == "" || req.Email == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "org_name, email and password required")
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not hash password")
			return
		}

		ctx := context.Background()
		tx, err := pool.Begin(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer tx.Rollback(ctx)

		var orgID string
		if err := tx.QueryRow(ctx,
			`INSERT INTO organisations (name) VALUES ($1) RETURNING id`, req.OrgName,
		).Scan(&orgID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create organisation")
			return
		}

		var userID string
		if err := tx.QueryRow(ctx,
			`INSERT INTO users (org_id, email, password_hash, role)
			 VALUES ($1,$2,$3,'admin') RETURNING id`,
			orgID, req.Email, string(hash),
		).Scan(&userID); err != nil {
			writeError(w, http.StatusConflict, "email already registered")
			return
		}

		if err := tx.Commit(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "commit failed")
			return
		}

		token, err := makeJWT(jwtSecret, userID, orgID, "admin")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "token error")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"token":   token,
			"org_id":  orgID,
			"user_id": userID,
			"role":    "admin",
		})
	}
}

func loginHandler(pool *pgxpool.Pool, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}

		var userID, orgID, role, pwHash string
		err := pool.QueryRow(context.Background(),
			`SELECT id, org_id, role, password_hash FROM users WHERE email=$1`, req.Email,
		).Scan(&userID, &orgID, &role, &pwHash)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(pwHash), []byte(req.Password)) != nil {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}

		token, err := makeJWT(jwtSecret, userID, orgID, role)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "token error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"token": token, "org_id": orgID, "role": role})
	}
}
