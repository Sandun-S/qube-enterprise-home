# 05 — TP-API: The Qube-Facing Interface

The TP-API (Transport Protocol API) runs on port 8081. Only Qubes talk to it. Humans and frontends never use it directly.

---

## Why a separate API?

| | Cloud API :8080 | TP-API :8081 |
|---|---|---|
| Used by | Frontend, IoT team | Qube devices |
| Auth | JWT (username + password) | HMAC token |
| Auth changes when | User logs out | Never (stable device identity) |
| Expiry | 24 hours | Never |

JWT tokens expire — you don't want a Qube to stop working because a token expired while it was offline for a week. HMAC tokens are computed from the device ID and org secret — they're always the same for the same device, never expire.

Separating the ports also lets you firewall port 8081 to only accept connections from Qube IP ranges.

---

## HMAC authentication

HMAC = Hash-based Message Authentication Code. It's a way to prove you know a secret without sending the secret itself.

```go
// cloud/internal/tpapi/router.go
func computeHMAC(qubeID, orgSecret string) string {
    mac := hmac.New(sha256.New, []byte(orgSecret))  // key = org's secret
    mac.Write([]byte(qubeID + ":" + orgSecret))     // data = device ID + secret
    return hex.EncodeToString(mac.Sum(nil))
    // Returns e.g. "c6a3ae484907a346e7f9..."
}
```

When a customer claims a device:
```
orgSecret = "abc123..." (random, stored in organisations table)
qubeID    = "Q-1001"
HMAC      = sha256(orgSecret, "Q-1001:abc123...")
          = "c6a3ae484907a346..."
```

This token is stored in `qubes.auth_token_hash` and given to the device. The device sends it on every TP-API request.

When validating:
```go
func qubeAuthMiddleware(pool *pgxpool.Pool) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            qubeID := r.Header.Get("X-Qube-ID")        // "Q-1001"
            token  := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
            
            // Fetch the org secret for this device
            var orgSecret, orgID string
            pool.QueryRow(ctx,
                `SELECT o.org_secret, o.id
                 FROM qubes q JOIN organisations o ON o.id = q.org_id
                 WHERE q.id = $1 AND q.org_id IS NOT NULL`, qubeID,
            ).Scan(&orgSecret, &orgID)
            
            // Recompute what the token should be
            expected := computeHMAC(qubeID, orgSecret)
            
            // Compare — must use hmac.Equal to prevent timing attacks
            if !hmac.Equal([]byte(expected), []byte(token)) {
                writeError(w, http.StatusUnauthorized, "invalid qube token")
                return
            }
            
            // Pass qube_id and org_id down to the handler
            ctx := context.WithValue(r.Context(), ctxQubeID, qubeID)
            ctx = context.WithValue(ctx, ctxOrgID, orgID)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

Why `hmac.Equal` and not `==`? `==` in Go (and most languages) stops comparing as soon as it finds a mismatch — a timing attack can measure how long the comparison takes to figure out how many characters match. `hmac.Equal` always takes the same amount of time regardless of where the mismatch is.

---

## Self-registration endpoint

`POST /v1/device/register` is the one public TP-API endpoint (no auth):

```go
func deviceRegisterHandler(pool *pgxpool.Pool) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req struct {
            DeviceID    string `json:"device_id"`
            RegisterKey string `json:"register_key"`
        }
        json.NewDecoder(r.Body).Decode(&req)
        
        // Look up device — both device_id AND register_key must match
        var orgID *string   // nullable — nil means unclaimed
        var orgSecret string
        var claimed bool
        
        pool.QueryRow(ctx,
            `SELECT q.org_id,
                    COALESCE(o.org_secret, ''),
                    (q.org_id IS NOT NULL) AS claimed
             FROM qubes q
             LEFT JOIN organisations o ON o.id = q.org_id
             WHERE q.id = $1 AND q.register_key = $2`,
            req.DeviceID, req.RegisterKey,
        ).Scan(&orgID, &orgSecret, &claimed)
        
        if !claimed {
            // Customer hasn't registered device yet — tell device to wait
            writeJSON(w, http.StatusAccepted, map[string]any{
                "status": "pending",
                "retry_secs": 60,
            })
            return
        }
        
        // Compute the same HMAC token that was generated when customer claimed
        authToken := computeHMAC(req.DeviceID, orgSecret)
        
        writeJSON(w, http.StatusOK, map[string]any{
            "status":     "claimed",
            "qube_token": authToken,
        })
    }
}
```

Security analysis: an attacker who knows a `device_id` but not the `register_key` gets a 401. An attacker who guesses a `register_key` but it's for a different `device_id` also gets 401 (the WHERE clause requires both). The register_key is generated with `/dev/urandom` at flash time — 12 characters from base32 = ~60 bits of entropy = practically impossible to guess.

---

## Sync endpoints

```go
// GET /v1/sync/state — cheap hash check (runs every 30s on every Qube)
func syncStateHandler(pool *pgxpool.Pool) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        qubeID := r.Context().Value(ctxQubeID).(string)
        
        var hash string
        var updatedAt time.Time
        pool.QueryRow(ctx,
            `SELECT hash, generated_at FROM config_state WHERE qube_id = $1`,
            qubeID).Scan(&hash, &updatedAt)
        
        writeJSON(w, http.StatusOK, map[string]any{
            "hash":       hash,
            "updated_at": updatedAt,
        })
    }
}

// GET /v1/sync/config — full config (only called when hash changes)
func syncConfigHandler(pool *pgxpool.Pool) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Load all services + sensors + CSV rows for this qube
        // Build docker-compose.yml string
        // Build map of path → file content
        // Build sensor_map for enterprise-influx-to-sql
        writeJSON(w, http.StatusOK, map[string]any{
            "hash":               hash,
            "docker_compose_yml": composeYML,
            "csv_files":          csvFiles,  // {"configs/panel-a/config.csv": "...", ...}
            "sensor_map":         sensorMap, // {"Main_Meter.active_power_w": "uuid", ...}
        })
    }
}
```

The scale: if you have 100 Qubes polling every 30 seconds, that's ~200 requests per minute to `sync/state`. Each request is just `SELECT hash FROM config_state WHERE qube_id=$1` — one indexed lookup. Postgres handles this trivially.

Only when a hash changes (you add/modify a sensor) does the Qube call `sync/config`, which is heavier. But that happens rarely — not on every poll.

---

## Telemetry ingest

```go
// POST /v1/telemetry/ingest — enterprise-influx-to-sql calls this
func telemetryIngestHandler(pool *pgxpool.Pool) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req struct {
            Readings []struct {
                SensorID string  `json:"sensor_id"`
                FieldKey string  `json:"field_key"`
                Value    float64 `json:"value"`
                Unit     string  `json:"unit"`
            } `json:"readings"`
        }
        json.NewDecoder(r.Body).Decode(&req)
        
        if len(req.Readings) > 5000 {
            writeError(w, http.StatusBadRequest, "batch too large (max 5000)")
            return
        }
        
        // Use pgx Batch for efficiency — one round trip instead of N
        batch := &pgx.Batch{}
        for _, r := range req.Readings {
            batch.Queue(
                `INSERT INTO sensor_readings (sensor_id, field_key, value, unit)
                 VALUES ($1, $2, $3, $4)`,
                r.SensorID, r.FieldKey, r.Value, r.Unit)
        }
        
        results := pool.SendBatch(ctx, batch)
        defer results.Close()
        
        inserted, failed := 0, 0
        for i := 0; i < len(req.Readings); i++ {
            _, err := results.Exec()
            if err != nil { failed++ } else { inserted++ }
        }
        
        writeJSON(w, http.StatusOK, map[string]any{
            "inserted": inserted, "failed": failed})
    }
}
```

`pgx.Batch` sends all inserts in one network round-trip. For 100 readings, that's the difference between 100 × (network latency) and 1 × (network latency). On a LAN with 1ms latency: 100ms vs 1ms.

---

## Commands flow

```
User sends command:
  POST /api/v1/qubes/Q-1001/commands {"command":"restart_service","payload":{"service":"panel-a"}}
  → Inserts into qube_commands with status='pending'
  → Returns command_id immediately (202 Accepted)

Qube polls (30s):
  POST /v1/commands/poll
  → SELECT * FROM qube_commands WHERE qube_id=$1 AND status='pending'
  → Returns pending commands

Qube executes:
  runs docker service update --force qube_panel-a

Qube acknowledges:
  POST /v1/commands/{id}/ack {"status":"executed","result":{"output":"..."}}
  → UPDATE qube_commands SET status='executed', result=$result WHERE id=$1

User checks:
  GET /api/v1/commands/{id}
  → Returns {status:"executed", result:{...}}
```
