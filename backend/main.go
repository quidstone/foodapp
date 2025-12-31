package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq"
)

func main() {
	// Read DB config from environment
	dbHost := getenv("POSTGRES_HOST", "localhost")
	dbPort := getenv("POSTGRES_PORT", "5432")
	dbUser := getenv("POSTGRES_USER", "fooduser")
	dbPass := getenv("POSTGRES_PASSWORD", "foodpass")
	dbName := getenv("POSTGRES_DB", "fooddb")
	port := getenv("PORT", "8080")

	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		dbUser, dbPass, dbHost, dbPort, dbName,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("failed to open DB: %v", err)
	}
	defer db.Close()

	// simple ping with timeout
	db.SetConnMaxLifetime(time.Hour)
	if err := pingDB(db); err != nil {
		log.Fatalf("failed to connect to DB: %v", err)
	}
	log.Println("connected to Postgres")

	// Handlers
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	http.HandleFunc("/restaurants", func(w http.ResponseWriter, r *http.Request) {
		// Later: query DB. For now, just a placeholder
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"restaurants endpoint placeholder"}`))
	})

	log.Printf("listening on :%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func pingDB(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(
		context.Background(), 5*time.Second,
	)
	defer cancel()
	return db.PingContext(ctx)
}
