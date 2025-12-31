package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/quidstone/foodapp-backend/internal/db"
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

	sqlxDB, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		log.Fatalf("failed to open DB: %v", err)
	}
	defer sqlxDB.Close()

	// simple ping with timeout
	sqlxDB.SetConnMaxLifetime(time.Hour)
	if err := pingDB(sqlxDB); err != nil {
		log.Fatalf("failed to connect to DB: %v", err)
	}
	log.Println("connected to Postgres")

	// Initialize repositories
	restaurantRepo := db.NewRestaurantRepository(sqlxDB)
	userRepo := db.NewUserRepository(sqlxDB)

	// Handlers
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// GET /restaurants/open?datetime=2024-01-15T14:30:00Z
	// Returns all restaurants open at the given datetime
	http.HandleFunc("/restaurants/open", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse datetime query parameter
		datetimeStr := r.URL.Query().Get("datetime")
		if datetimeStr == "" {
			http.Error(w, "Missing required query parameter: datetime", http.StatusBadRequest)
			return
		}

		// Parse datetime (support RFC3339 format: 2024-01-15T14:30:00Z)
		datetime, err := time.Parse(time.RFC3339, datetimeStr)
		if err != nil {
			// Try alternative formats
			datetime, err = time.Parse("2006-01-02T15:04:05", datetimeStr)
			if err != nil {
				http.Error(w, fmt.Sprintf("Invalid datetime format. Use RFC3339 (e.g., 2024-01-15T14:30:00Z) or 2006-01-02T15:04:05: %v", err), http.StatusBadRequest)
				return
			}
		}

		// Query restaurants open at this datetime
		restaurants, err := restaurantRepo.FindOpenAtTime(datetime)
		if err != nil {
			log.Printf("Error querying open restaurants: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Return JSON response
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(restaurants); err != nil {
			log.Printf("Error encoding response: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	})

	// GET /restaurants/top?limit=10&dish_count=5&min_price=10&max_price=50&comparison=more
	// Returns top y restaurants that have more/less than x dishes within price range, ranked alphabetically
	http.HandleFunc("/restaurants/top", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse query parameters
		limitStr := r.URL.Query().Get("limit")
		dishCountStr := r.URL.Query().Get("dish_count")
		minPriceStr := r.URL.Query().Get("min_price")
		maxPriceStr := r.URL.Query().Get("max_price")
		comparison := r.URL.Query().Get("comparison")

		if limitStr == "" || dishCountStr == "" || minPriceStr == "" || maxPriceStr == "" {
			http.Error(w, "Missing required query parameters: limit, dish_count, min_price, max_price", http.StatusBadRequest)
			return
		}

		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit <= 0 {
			http.Error(w, "Invalid limit parameter", http.StatusBadRequest)
			return
		}

		dishCount, err := strconv.Atoi(dishCountStr)
		if err != nil || dishCount < 0 {
			http.Error(w, "Invalid dish_count parameter", http.StatusBadRequest)
			return
		}

		minPrice, err := strconv.ParseFloat(minPriceStr, 64)
		if err != nil || minPrice < 0 {
			http.Error(w, "Invalid min_price parameter", http.StatusBadRequest)
			return
		}

		maxPrice, err := strconv.ParseFloat(maxPriceStr, 64)
		if err != nil || maxPrice < 0 || maxPrice < minPrice {
			http.Error(w, "Invalid max_price parameter", http.StatusBadRequest)
			return
		}

		if comparison == "" {
			comparison = "more" // default
		}

		// Query restaurants
		restaurants, err := restaurantRepo.FindTopByDishCount(limit, dishCount, minPrice, maxPrice, comparison)
		if err != nil {
			log.Printf("Error querying top restaurants: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Return JSON response
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(restaurants); err != nil {
			log.Printf("Error encoding response: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	})

	// GET /search?q=pizza&limit=20
	// Searches for restaurants and dishes by name, ranked by relevance
	http.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		queryTerm := r.URL.Query().Get("q")
		if queryTerm == "" {
			http.Error(w, "Missing required query parameter: q", http.StatusBadRequest)
			return
		}

		limitStr := r.URL.Query().Get("limit")
		limit := 20 // default
		if limitStr != "" {
			var err error
			limit, err = strconv.Atoi(limitStr)
			if err != nil || limit <= 0 {
				http.Error(w, "Invalid limit parameter", http.StatusBadRequest)
				return
			}
		}

		// Search restaurants and dishes
		results, err := restaurantRepo.Search(queryTerm, limit)
		if err != nil {
			log.Printf("Error searching: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Return JSON response
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(results); err != nil {
			log.Printf("Error encoding response: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	})

	// POST /purchase
	// Processes a user purchasing a dish from a restaurant
	// Body: {"user_id": 1, "menu_item_id": 123, "quantity": 2}
	http.HandleFunc("/purchase", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse request body
		var purchaseReq db.PurchaseRequest
		if err := json.NewDecoder(r.Body).Decode(&purchaseReq); err != nil {
			http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
			return
		}

		// Validate required fields
		if purchaseReq.UserID <= 0 {
			http.Error(w, "Invalid user_id", http.StatusBadRequest)
			return
		}
		if purchaseReq.MenuItemID <= 0 {
			http.Error(w, "Invalid menu_item_id", http.StatusBadRequest)
			return
		}
		if purchaseReq.Quantity <= 0 {
			http.Error(w, "Invalid quantity (must be > 0)", http.StatusBadRequest)
			return
		}

		// Process purchase
		result, err := userRepo.PurchaseDish(purchaseReq)
		if err != nil {
			// Check for specific error types
			if err.Error() == "user not found" ||
				err.Error() == "menu item not found" ||
				err.Error() == "menu item is not active" {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			if err.Error()[:18] == "insufficient balance" {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			log.Printf("Error processing purchase: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Return success response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(result); err != nil {
			log.Printf("Error encoding response: %v", err)
			return
		}
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

func pingDB(db *sqlx.DB) error {
	ctx, cancel := context.WithTimeout(
		context.Background(), 5*time.Second,
	)
	defer cancel()
	return db.PingContext(ctx)
}
