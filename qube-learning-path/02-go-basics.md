# 02 — Go Basics: What You Need for This Codebase

Go is used for all four services: cloud-api, conf-agent, enterprise-influx-to-sql, mqtt-gateway.

---

## Why Go for this project

- **Single binary**: `go build` produces one executable. No runtime dependencies. Easy to deploy inside a Docker image — the final image can be just Alpine Linux + the binary.
- **Fast startup**: Containers start in milliseconds. Important for Docker Swarm which restarts services frequently.
- **stdlib HTTP**: Go's `net/http` package is production-ready without a framework. The conf-agent uses zero external HTTP dependencies.
- **Cross-compile**: `GOOS=linux GOARCH=arm64 go build` on your Windows machine produces an ARM64 binary for the Qube. This is how CI/CD builds arm64 images on GitHub's amd64 runners.

---

## Package structure

Every `.go` file starts with `package name`. Files in the same directory share a package:

```go
// cloud/internal/api/auth.go
package api

// cloud/internal/api/qubes.go
package api
// These two files share all functions and types
```

The `internal` directory is special — code inside it can only be used by the parent module. `cloud/internal/api` can only be imported by code inside the `cloud` module, not by outside code. This enforces that the API handlers are private to the cloud-api binary.

---

## How HTTP handlers work

This is the core pattern used everywhere in the codebase:

```go
// A handler is a function that takes a pool (database connection)
// and returns an http.HandlerFunc
func createGatewayHandler(pool *pgxpool.Pool) http.HandlerFunc {
    // This outer function runs ONCE at startup — sets up anything expensive
    
    // Return the actual handler — this runs on every HTTP request
    return func(w http.ResponseWriter, r *http.Request) {
        // w = response writer (write your response here)
        // r = the request (read body, headers, URL params here)
        
        // Read the request body
        var req struct {
            Name     string `json:"name"`
            Protocol string `json:"protocol"`
        }
        json.NewDecoder(r.Body).Decode(&req)
        
        // Query the database
        var id string
        pool.QueryRow(context.Background(),
            `INSERT INTO gateways (name, protocol) VALUES ($1, $2) RETURNING id`,
            req.Name, req.Protocol,
        ).Scan(&id)
        
        // Write the response
        json.NewEncoder(w).Encode(map[string]string{"id": id})
    }
}
```

Why return a function from a function? Because the `pool` variable is captured in a closure. Each request uses the same pool without passing it around everywhere.

---

## Context

`context.Background()` appears everywhere in database calls:

```go
pool.QueryRow(context.Background(), `SELECT ...`, arg)
```

Context is Go's way of passing deadlines and cancellation through call chains. `context.Background()` means "no deadline, never cancel". In a production system you'd use `r.Context()` (the request's context) so DB queries cancel automatically if the client disconnects. For this project, `context.Background()` is fine.

---

## Error handling

Go has no exceptions. Errors are return values:

```go
// Functions return (result, error)
rows, err := pool.Query(ctx, `SELECT ...`)
if err != nil {
    // Handle the error — usually write an HTTP error and return
    writeError(w, http.StatusInternalServerError, "db error")
    return
}
defer rows.Close()  // always close rows when done
```

The `defer` keyword runs a function when the surrounding function exits. `defer rows.Close()` means "close rows no matter what happens — even if we return early due to an error."

---

## Structs and JSON

Go structs with backtick tags control JSON encoding/decoding:

```go
var req struct {
    Name        string `json:"name"`           // maps to "name" in JSON
    Protocol    string `json:"protocol"`
    AddressParams any  `json:"address_params"` // any = any JSON type
    TagsJSON    any    `json:"tags_json"`
}
json.NewDecoder(r.Body).Decode(&req)  // & means "pointer to req"
```

`any` (same as `interface{}`) means the field can hold any JSON value — object, array, string, number. Used when the shape varies by protocol.

---

## The `pool` — database connection

```go
// pgxpool.Pool is a thread-safe pool of Postgres connections
// Created once at startup, shared across all requests
pool, err := pgxpool.New(context.Background(), databaseURL)
```

Three ways to query:

```go
// 1. Single row — use QueryRow + Scan
var id, name string
pool.QueryRow(ctx, `SELECT id, name FROM qubes WHERE id=$1`, qubeID).Scan(&id, &name)

// 2. Multiple rows — use Query + for rows.Next()
rows, _ := pool.Query(ctx, `SELECT id, name FROM qubes WHERE org_id=$1`, orgID)
defer rows.Close()
for rows.Next() {
    var id, name string
    rows.Scan(&id, &name)
    // process each row
}

// 3. No result needed (INSERT/UPDATE/DELETE) — use Exec
pool.Exec(ctx, `UPDATE qubes SET status='online' WHERE id=$1`, qubeID)
```

`$1`, `$2` are Postgres positional parameters — never concatenate user input into SQL (SQL injection).

---

## Goroutines and the poll loop

The conf-agent uses a ticker to run code every 30 seconds:

```go
ticker := time.NewTicker(30 * time.Second)
defer ticker.Stop()

// Run immediately on start
runCycle(client, cfg, &localHash, hashFile)

// Then run every 30s
for range ticker.C {
    runCycle(client, cfg, &localHash, hashFile)
}
```

`ticker.C` is a channel. `for range ticker.C` blocks until the ticker sends a value (every 30s), then runs the loop body. This is clean and doesn't use `sleep` in a loop.

---

## Reading files

The conf-agent reads `/boot/mit.txt` using Go's `bufio.Scanner`:

```go
f, err := os.Open("/boot/mit.txt")
defer f.Close()

scanner := bufio.NewScanner(f)
for scanner.Scan() {
    line := scanner.Text()           // "deviceid: Qube-1302"
    parts := strings.SplitN(line, ":", 2)
    key := strings.TrimSpace(parts[0])   // "deviceid"
    val := strings.TrimSpace(parts[1])   // "Qube-1302"
    
    switch key {
    case "deviceid":
        m.DeviceID = val
    case "register":
        m.RegisterKey = val
    }
}
```

`SplitN(line, ":", 2)` splits on `:` but at most 2 parts — so `"opc.tcp://host:4840"` doesn't get split at the second colon.

---

## JSON marshaling for dynamic data

The CSV rows are stored as `JSONB` in Postgres. Each row's structure varies by protocol:

```go
// A Modbus row looks like:
map[string]any{
    "Equipment": "Main_Meter",
    "Reading":   "active_power_w",
    "Address":   3000,
    "Type":      "uint16",
}

// Marshal to bytes for storage
rowBytes, _ := json.Marshal(row)
pool.Exec(ctx,
    `INSERT INTO service_csv_rows (row_data) VALUES ($1)`, rowBytes)

// Later, unmarshal back
var data map[string]any
json.Unmarshal(rawBytes, &data)
address := data["Address"]  // returns float64 (JSON numbers are always float64)
addrInt := int(address.(float64))  // type assert then convert
```

`.(float64)` is a type assertion — it says "I know this is a float64, give it to me as one." If it's not float64, it panics. Use `v, ok := data["Address"].(float64)` for safe assertion.

---

## The `any` type and type assertions

`any` = can hold any value. You must type-assert to use it:

```go
var v any = map[string]any{"key": "value"}

// Safe assertion (won't panic):
m, ok := v.(map[string]any)
if ok {
    fmt.Println(m["key"])  // "value"
}

// The strVal helper used throughout sensors.go:
func strVal(v any, def string) string {
    if s, ok := v.(string); ok && s != "" {
        return s
    }
    return def  // return default if nil, wrong type, or empty
}
```

---

## What to study next

Read these files in order:
1. `cloud/cmd/server/main.go` — how the server starts, what env vars it reads
2. `cloud/internal/api/router.go` — all routes in one place
3. `cloud/internal/api/middleware.go` — how JWT auth works
4. `cloud/internal/api/auth.go` — register and login
5. `conf-agent/main.go` — the whole edge agent in one file
