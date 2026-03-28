package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qube-enterprise/cloud/internal/api"
	"github.com/qube-enterprise/cloud/internal/tpapi"
)

func main() {
	dbURL := getenv("DATABASE_URL",
		"postgres://qubeadmin:qubepass@127.0.0.1:5432/qubedb?sslmode=disable")
	telemetryDBURL := getenv("TELEMETRY_DATABASE_URL",
		"postgres://qubeadmin:qubepass@127.0.0.1:5432/qubedata?sslmode=disable")
	jwtSecret := getenv("JWT_SECRET", "dev-jwt-secret-change-in-production")

	ctx := context.Background()

	// Management database (qubedb)
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("cannot connect to management database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("management database ping failed: %v", err)
	}
	log.Println("Management database connected (qubedb)")

	// Telemetry database (qubedata — TimescaleDB)
	telemetryPool, err := pgxpool.New(ctx, telemetryDBURL)
	if err != nil {
		log.Fatalf("cannot connect to telemetry database: %v", err)
	}
	if err := telemetryPool.Ping(ctx); err != nil {
		log.Fatalf("telemetry database ping failed: %v", err)
	}
	log.Println("Telemetry database connected (qubedata)")

	// TP-API on :8081 (Qube-facing — HMAC auth)
	go func() {
		log.Println("TP-API  listening on :8081")
		if err := http.ListenAndServe(":8081", tpapi.NewRouter(pool, telemetryPool)); err != nil {
			log.Fatalf("TP-API failed: %v", err)
		}
	}()

	// Cloud API on :8080 (user/UI facing — JWT auth + WebSocket)
	log.Println("Cloud API listening on :8080")
	if err := http.ListenAndServe(":8080", api.NewRouter(pool, telemetryPool, jwtSecret)); err != nil {
		log.Fatalf("Cloud API failed: %v", err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
