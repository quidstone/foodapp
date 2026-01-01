package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/quidstone/foodapp-backend/internal/db"
	"github.com/quidstone/foodapp-backend/internal/metrics"
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

	// Initialize metrics collector
	metricsCollector := metrics.NewMetricsCollector()

	// Initialize cache (optional - set to nil to disable caching)
	// Default TTL: 5 minutes, Cleanup interval: 1 minute
	var cache db.Cache
	if getenv("ENABLE_CACHE", "false") == "true" {
		cacheTTL := 5 * time.Minute
		cleanupInterval := 1 * time.Minute
		cache = db.NewMemoryCache(cacheTTL, cleanupInterval)
		log.Println("database query cache enabled")
	}

	// Wrap database with metrics tracking and optional caching
	var wrappedDB *db.DBWrapper
	if cache != nil {
		wrappedDB = db.NewDBWrapperWithCache(sqlxDB, metricsCollector, cache, 5*time.Minute)
	} else {
		wrappedDB = db.NewDBWrapper(sqlxDB, metricsCollector)
	}

	// Initialize repositories with wrapped DB
	restaurantRepo := db.NewRestaurantRepository(wrappedDB)
	userRepo := db.NewUserRepository(wrappedDB)

	// Create a mux for applying middleware
	mux := http.NewServeMux()

	// Handlers
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		// Check database connectivity with a timeout
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := sqlxDB.PingContext(ctx); err != nil {
			log.Printf("Health check failed: database ping error: %v", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("database unavailable"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// GET /metrics - Returns collected metrics
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		metricsData := map[string]interface{}{
			"api_metrics": metricsCollector.GetAPIMetrics(),
			"db_metrics":  metricsCollector.GetDBMetrics(),
		}
		if err := json.NewEncoder(w).Encode(metricsData); err != nil {
			log.Printf("Error encoding metrics: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	})

	// GET /restaurants/open?datetime=2024-01-15T14:30:00Z
	// Returns all restaurants open at the given datetime
	mux.HandleFunc("/restaurants/open", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/restaurants/top", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
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
	// Processes a user purchasing dishes from a restaurant
	// Body: {"user_id": 1, "items": [{"menu_item_id": 123, "quantity": 2}, {"menu_item_id": 456, "quantity": 1}]}
	// Header: Idempotency-Key (optional) - UUID to prevent duplicate orders on retry
	mux.HandleFunc("/purchase", func(w http.ResponseWriter, r *http.Request) {
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

		// Extract idempotency key from header (optional)
		purchaseReq.IdempotencyKey = r.Header.Get("Idempotency-Key")

		// Validate required fields
		if purchaseReq.UserID <= 0 {
			http.Error(w, "Invalid user_id", http.StatusBadRequest)
			return
		}

		// Validate items
		if len(purchaseReq.Items) == 0 {
			http.Error(w, "items array cannot be empty", http.StatusBadRequest)
			return
		}
		for i, item := range purchaseReq.Items {
			if item.MenuItemID <= 0 {
				http.Error(w, fmt.Sprintf("Invalid menu_item_id at index %d", i), http.StatusBadRequest)
				return
			}
			if item.Quantity <= 0 {
				http.Error(w, fmt.Sprintf("Invalid quantity at index %d (must be > 0)", i), http.StatusBadRequest)
				return
			}
		}

		// Process purchase
		result, err := userRepo.PurchaseDish(purchaseReq)
		if err != nil {
			errMsg := err.Error()
			// Check for specific error types
			switch {
			case errMsg == "user not found",
				strings.HasPrefix(errMsg, "menu item not found"),
				strings.HasPrefix(errMsg, "menu item is not active"),
				strings.HasPrefix(errMsg, "no items specified"):
				http.Error(w, errMsg, http.StatusNotFound)
				return
			case strings.HasPrefix(errMsg, "insufficient balance"):
				http.Error(w, errMsg, http.StatusBadRequest)
				return
			default:
				log.Printf("Error processing purchase: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
		}

		// Return success response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(result); err != nil {
			log.Printf("Error encoding response: %v", err)
			return
		}
	})

	// Apply metrics middleware to all routes
	handler := metrics.Middleware(metricsCollector)(mux)

	log.Printf("listening on :%s\n", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
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
