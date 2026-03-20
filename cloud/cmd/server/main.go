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
	jwtSecret := getenv("JWT_SECRET", "dev-jwt-secret-change-in-production")

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		log.Fatalf("cannot connect to database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("database ping failed: %v", err)
	}
	log.Println("Database connected")

	// TP-API on :8081 (Qube-facing — no JWT, uses HMAC token)
	go func() {
		log.Println("TP-API  listening on :8081")
		if err := http.ListenAndServe(":8081", tpapi.NewRouter(pool)); err != nil {
			log.Fatalf("TP-API failed: %v", err)
		}
	}()

	// Cloud API on :8080 (user/UI facing — JWT auth)
	log.Println("Cloud API listening on :8080")
	if err := http.ListenAndServe(":8080", api.NewRouter(pool, jwtSecret)); err != nil {
		log.Fatalf("Cloud API failed: %v", err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
