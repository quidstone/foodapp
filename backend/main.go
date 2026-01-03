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
	"github.com/quidstone/foodapp-backend/internal/api"
	"github.com/quidstone/foodapp-backend/internal/db"
	"github.com/quidstone/foodapp-backend/internal/metrics"
	"github.com/shopspring/decimal"
)

// Comparison operators for restaurant dish count filtering
const (
	ComparisonMore = "more"
	ComparisonLess = "less"
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

	// Wrap database with metrics tracking
	wrappedDB := db.NewDBWrapper(sqlxDB, metricsCollector)

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
			api.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		metricsData := map[string]interface{}{
			"api_metrics": metricsCollector.GetAPIMetrics(),
			"db_metrics":  metricsCollector.GetDBMetrics(),
		}
		api.WriteResponse(w, metricsData, http.StatusOK)
	})

	// GET /restaurants/open?datetime=2024-01-15T14:30:00Z
	// Returns all restaurants open at the given datetime
	mux.HandleFunc("/restaurants/open", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			api.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		// Parse datetime query parameter
		datetimeStr := r.URL.Query().Get("datetime")
		if datetimeStr == "" {
			api.WriteError(w, http.StatusBadRequest, "Missing required query parameter: datetime")
			return
		}

		// Parse datetime (support RFC3339 format: 2024-01-15T14:30:00Z)
		datetime, err := time.Parse(time.RFC3339, datetimeStr)
		if err != nil {
			// Try alternative formats
			datetime, err = time.Parse("2006-01-02T15:04:05", datetimeStr)
			if err != nil {
				api.WriteError(w, http.StatusBadRequest, fmt.Sprintf("Invalid datetime format. Use RFC3339 (e.g., 2024-01-15T14:30:00Z) or 2006-01-02T15:04:05: %v", err))
				return
			}
		}

		// Query restaurants open at this datetime
		restaurants, err := restaurantRepo.FindOpenAtTime(datetime)
		if err != nil {
			log.Printf("Error querying open restaurants: %v", err)
			api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return
		}

		// Return JSON response
		api.WriteResponse(w, restaurants, http.StatusOK)
	})

	// GET /restaurants/top?limit=10&dish_count=5&min_price=10&max_price=50&comparison=more
	// Returns top y restaurants that have more/less than x dishes within price range, ranked alphabetically
	mux.HandleFunc("/restaurants/top", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			api.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		// Parse query parameters
		limitStr := r.URL.Query().Get("limit")
		dishCountStr := r.URL.Query().Get("dish_count")
		minPriceStr := r.URL.Query().Get("min_price")
		maxPriceStr := r.URL.Query().Get("max_price")
		comparison := r.URL.Query().Get("comparison")

		if limitStr == "" || dishCountStr == "" || minPriceStr == "" || maxPriceStr == "" {
			api.WriteError(w, http.StatusBadRequest, "Missing required query parameters: limit, dish_count, min_price, max_price")
			return
		}

		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit <= 0 {
			api.WriteError(w, http.StatusBadRequest, "Invalid limit parameter")
			return
		}

		dishCount, err := strconv.Atoi(dishCountStr)
		if err != nil || dishCount < 0 {
			api.WriteError(w, http.StatusBadRequest, "Invalid dish_count parameter")
			return
		}

		minPrice, err := strconv.ParseFloat(minPriceStr, 64)
		if err != nil || minPrice < 0 {
			api.WriteError(w, http.StatusBadRequest, "Invalid min_price parameter")
			return
		}

		maxPrice, err := strconv.ParseFloat(maxPriceStr, 64)
		if err != nil || maxPrice < 0 || maxPrice < minPrice {
			api.WriteError(w, http.StatusBadRequest, "Invalid max_price parameter")
			return
		}

		if comparison == "" {
			comparison = ComparisonMore // default
		} else if comparison != ComparisonMore && comparison != ComparisonLess {
			api.WriteError(w, http.StatusBadRequest, fmt.Sprintf("Invalid comparison parameter. Must be '%s' or '%s'", ComparisonMore, ComparisonLess))
			return
		}

		// Query restaurants
		restaurants, err := restaurantRepo.FindTopByDishCount(limit, dishCount, decimal.NewFromFloat(minPrice), decimal.NewFromFloat(maxPrice), comparison)
		if err != nil {
			log.Printf("Error querying top restaurants: %v", err)
			api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return
		}

		// Return JSON response
		api.WriteResponse(w, restaurants, http.StatusOK)
	})

	// GET /search?q=pizza&limit=20
	// Searches for restaurants and dishes by name, ranked by relevance
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			api.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		queryTerm := r.URL.Query().Get("q")
		if queryTerm == "" {
			api.WriteError(w, http.StatusBadRequest, "Missing required query parameter: q")
			return
		}

		limitStr := r.URL.Query().Get("limit")
		limit := 20 // default
		if limitStr != "" {
			var err error
			limit, err = strconv.Atoi(limitStr)
			if err != nil || limit <= 0 {
				api.WriteError(w, http.StatusBadRequest, "Invalid limit parameter")
				return
			}
		}

		// Search restaurants and dishes
		results, err := restaurantRepo.Search(queryTerm, limit)
		if err != nil {
			log.Printf("Error searching: %v", err)
			api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return
		}

		// Return JSON response
		api.WriteResponse(w, results, http.StatusOK)
	})

	// POST /purchase
	// Processes a user purchasing dishes from a restaurant
	// Body: {"user_id": 1, "items": [{"menu_item_id": 123, "quantity": 2}, {"menu_item_id": 456, "quantity": 1}]}
	// Header: Idempotency-Key (optional) - UUID to prevent duplicate orders on retry
	mux.HandleFunc("/purchase", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			api.WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		// Parse request body
		var purchaseReq api.PurchaseRequest
		if err := json.NewDecoder(r.Body).Decode(&purchaseReq); err != nil {
			api.WriteError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
			return
		}

		// Extract idempotency key from header (optional)
		purchaseReq.IdempotencyKey = r.Header.Get("Idempotency-Key")

		// Validate required fields
		if purchaseReq.UserID <= 0 {
			api.WriteError(w, http.StatusBadRequest, "Invalid user_id")
			return
		}

		// Validate items
		if len(purchaseReq.Items) == 0 {
			api.WriteError(w, http.StatusBadRequest, "items array cannot be empty")
			return
		}
		for i, item := range purchaseReq.Items {
			if item.MenuItemID <= 0 {
				api.WriteError(w, http.StatusBadRequest, fmt.Sprintf("Invalid menu_item_id at index %d", i))
				return
			}
			if item.Quantity <= 0 {
				api.WriteError(w, http.StatusBadRequest, fmt.Sprintf("Invalid quantity at index %d (must be > 0)", i))
				return
			}
		}

		// Map API Request to DB Params
		dbItems := make([]db.PurchaseItem, len(purchaseReq.Items))
		for i, item := range purchaseReq.Items {
			dbItems[i] = db.PurchaseItem{
				MenuItemID: item.MenuItemID,
				Quantity:   item.Quantity,
			}
		}

		purchaseParams := db.PurchaseParams{
			UserID:         purchaseReq.UserID,
			Items:          dbItems,
			IdempotencyKey: purchaseReq.IdempotencyKey,
		}

		// Process purchase
		result, err := userRepo.PurchaseDish(purchaseParams)
		if err != nil {
			errMsg := err.Error()
			// Check for specific error types
			switch {
			case errMsg == "user not found",
				strings.HasPrefix(errMsg, "menu item not found"),
				strings.HasPrefix(errMsg, "menu item is not active"),
				strings.HasPrefix(errMsg, "no items specified"):
				api.WriteError(w, http.StatusNotFound, errMsg)
				return
			case strings.HasPrefix(errMsg, "insufficient balance"):
				api.WriteError(w, http.StatusBadRequest, errMsg)
				return
			default:
				log.Printf("Error processing purchase: %v", err)
				api.WriteError(w, http.StatusInternalServerError, "Internal server error")
				return
			}
		}

		// Map DB Result to API Result
		apiResult := api.PurchaseResult{
			OrderID:      result.OrderID,
			UserID:       result.UserID,
			RestaurantID: result.RestaurantID,
			TotalAmount:  result.TotalAmount,
			Message:      result.Message,
		}

		// Return success response
		api.WriteResponse(w, apiResult, http.StatusOK)
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
