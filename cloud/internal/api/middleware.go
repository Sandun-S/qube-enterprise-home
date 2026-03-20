package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type ctxKey string

const (
	ctxUserID ctxKey = "user_id"
	ctxOrgID  ctxKey = "org_id"
	ctxRole   ctxKey = "role"
)

func jwtMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "missing or invalid Authorization header")
				return
			}
			tok, err := jwt.Parse(
				strings.TrimPrefix(auth, "Bearer "),
				func(t *jwt.Token) (any, error) { return []byte(secret), nil },
				jwt.WithValidMethods([]string{"HS256"}),
			)
			if err != nil || !tok.Valid {
				writeError(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}
			claims := tok.Claims.(jwt.MapClaims)
			ctx := context.WithValue(r.Context(), ctxUserID, claims["user_id"])
			ctx = context.WithValue(ctx, ctxOrgID, claims["org_id"])
			ctx = context.WithValue(ctx, ctxRole, claims["role"])
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// requireRole accepts multiple allowed roles — any match allows access.
// Role hierarchy: superadmin > admin > editor > viewer
func requireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles { allowed[r] = true }
	// superadmin always passes — they can do everything
	allowed["superadmin"] = true

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, _ := r.Context().Value(ctxRole).(string)
			if allowed[role] {
				next.ServeHTTP(w, r)
				return
			}
			writeError(w, http.StatusForbidden,
				"insufficient role — need one of: "+strings.Join(roles, ", "))
		})
	}
}

func makeJWT(secret, userID, orgID, role string) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"org_id":  orgID,
		"role":    role,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	}).SignedString([]byte(secret))
}
