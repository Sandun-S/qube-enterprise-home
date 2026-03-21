# 04 — Cloud API: How Requests Are Handled

The Cloud API is a standard Go HTTP server. Every request goes: `router → middleware → handler → database → response`.

File: `cloud/internal/api/`

---

## How it starts

```go
// cloud/cmd/server/main.go
func main() {
    // 1. Connect to Postgres
    pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
    
    // 2. Get JWT secret from env
    jwtSecret := os.Getenv("JWT_SECRET")
    
    // 3. Create routers
    cloudRouter := api.NewRouter(pool, jwtSecret)  // port 8080
    tpRouter    := tpapi.NewRouter(pool)            // port 8081
    
    // 4. Start both servers concurrently
    go http.ListenAndServe(":8080", cloudRouter)
    http.ListenAndServe(":8081", tpRouter)
}
```

One binary, two HTTP servers, different ports. `go http.ListenAndServe(...)` starts the first server in a goroutine (non-blocking), then the second blocks the main goroutine.

---

## Routing: chi

```go
// cloud/internal/api/router.go
r := chi.NewRouter()
r.Use(middleware.Logger)     // logs every request
r.Use(middleware.Recoverer)  // catches panics, returns 500

// Public routes
r.Post("/api/v1/auth/register", registerHandler(pool, jwtSecret))
r.Post("/api/v1/auth/login",    loginHandler(pool, jwtSecret))

// Protected routes — JWT required
r.Group(func(r chi.Router) {
    r.Use(jwtMiddleware(jwtSecret))  // runs before every handler in this group
    
    r.Get("/api/v1/qubes", listQubesHandler(pool))
    // ...
    
    // Admin-only routes — nested group with extra middleware
    r.Group(func(r chi.Router) {
        r.Use(requireRole("admin", "superadmin"))
        r.Post("/api/v1/qubes/claim", claimQubeHandler(pool))
    })
})
```

`r.Group()` creates a sub-router that inherits the parent's middleware AND adds more. The outer group adds JWT auth. The inner group adds role check. Both run for admin routes.

Chi URL parameters: `{id}` in the path becomes accessible with `chi.URLParam(r, "id")`.

---

## JWT middleware

```go
// cloud/internal/api/middleware.go
func jwtMiddleware(secret string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // 1. Extract token from Authorization header
            auth := r.Header.Get("Authorization")
            if !strings.HasPrefix(auth, "Bearer ") {
                writeError(w, http.StatusUnauthorized, "missing or invalid Authorization header")
                return
            }
            token := strings.TrimPrefix(auth, "Bearer ")
            
            // 2. Parse and validate JWT
            tok, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
                return []byte(secret), nil  // returns the signing key
            }, jwt.WithValidMethods([]string{"HS256"}))
            
            if err != nil || !tok.Valid {
                writeError(w, http.StatusUnauthorized, "invalid or expired token")
                return
            }
            
            // 3. Extract claims and put them in context
            claims := tok.Claims.(jwt.MapClaims)
            ctx := context.WithValue(r.Context(), ctxUserID, claims["user_id"])
            ctx = context.WithValue(ctx, ctxOrgID,  claims["org_id"])
            ctx = context.WithValue(ctx, ctxRole,   claims["role"])
            
            // 4. Pass to next handler with enriched context
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

The context is the Go way to pass request-scoped values down the call chain. The handler reads them back:
```go
orgID, _ := r.Context().Value(ctxOrgID).(string)
role, _  := r.Context().Value(ctxRole).(string)
```

---

## Auth: register and login

```go
// cloud/internal/api/auth.go
func registerHandler(pool *pgxpool.Pool, jwtSecret string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req struct {
            OrgName  string `json:"org_name"`
            Email    string `json:"email"`
            Password string `json:"password"`
        }
        json.NewDecoder(r.Body).Decode(&req)
        
        // Use a transaction — org + user must be created together or not at all
        tx, _ := pool.Begin(ctx)
        defer tx.Rollback(ctx)  // rolls back if we don't commit
        
        // Create org
        var orgID string
        tx.QueryRow(ctx, `INSERT INTO organisations (name) VALUES ($1) RETURNING id`,
            req.OrgName).Scan(&orgID)
        
        // Hash password using pgcrypto — stays in database, never in Go memory
        var userID string
        tx.QueryRow(ctx,
            `INSERT INTO users (org_id, email, password_hash, role)
             VALUES ($1, $2, crypt($3, gen_salt('bf',12)), 'admin') RETURNING id`,
            orgID, req.Email, req.Password).Scan(&userID)
        
        tx.Commit(ctx)
        
        // Create JWT
        token, _ := makeJWT(jwtSecret, userID, orgID, "admin")
        writeJSON(w, http.StatusCreated, map[string]any{"token": token, "org_id": orgID})
    }
}
```

`crypt($3, gen_salt('bf',12))` — pgcrypto generates a bcrypt hash entirely in Postgres. The plaintext password never needs to be handled in Go code. Login verifies with `password_hash = crypt($password, password_hash)`.

---

## Org isolation: every query filters by org_id

Every handler that returns data filters by `org_id` from the JWT claims. A user can never see another org's data:

```go
func listGatewaysHandler(pool *pgxpool.Pool) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        orgID, _ := r.Context().Value(ctxOrgID).(string)  // from JWT
        qubeID := chi.URLParam(r, "id")
        
        // The JOIN with qubes ensures the qube belongs to this org
        rows, err := pool.Query(ctx,
            `SELECT g.id, g.name, g.protocol, g.host
             FROM gateways g
             JOIN qubes q ON q.id = g.qube_id
             WHERE g.qube_id = $1 AND q.org_id = $2`,  // ← both conditions
            qubeID, orgID)
        // If qubeID belongs to a different org, this returns 0 rows
        // The 404 handler above returns "not found" — not "forbidden"
        // This avoids leaking information about whether the qube exists
    }
}
```

Returning "not found" instead of "forbidden" for wrong-org access is intentional — it prevents an attacker from enumerating which qubes exist.

---

## Response helpers

```go
func writeJSON(w http.ResponseWriter, status int, v any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
    writeJSON(w, status, map[string]string{"error": msg})
}
```

Always call `w.WriteHeader()` BEFORE writing the body. Once you write the body, the status code is locked. Calling `w.Header().Set()` after `w.WriteHeader()` has no effect.

---

## The registry API (new)

```bash
# Check current registry mode
curl -H "Authorization: Bearer $SUPER_TOKEN" \
  http://cloud:8080/api/v1/admin/registry | jq .

# Switch to GitHub mode
curl -X PUT -H "Authorization: Bearer $SUPER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"github","github_base":"ghcr.io/sandun-s/qube-enterprise-home"}' \
  http://cloud:8080/api/v1/admin/registry

# Switch to GitLab mode
curl -X PUT -H "Authorization: Bearer $SUPER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"gitlab","gitlab_base":"registry.gitlab.com/iot-team4/product"}' \
  http://cloud:8080/api/v1/admin/registry

# Custom: set one specific image
curl -X PUT -H "Authorization: Bearer $SUPER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"img_modbus":"my-registry.com/my-modbus:v2.1"}' \
  http://cloud:8080/api/v1/admin/registry
```

The setting is stored in Postgres, so it persists across restarts. The next time conf-agent syncs, it gets a `docker-compose.yml` with the updated image paths.
