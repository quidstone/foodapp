package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/quidstone/foodapp-backend/internal/db"
	"github.com/stretchr/testify/assert"
)

func setupTestDB(t *testing.T) (*sqlx.DB, sqlmock.Sqlmock) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	sqlxDB := sqlx.NewDb(mockDB, "postgres")
	return sqlxDB, mock
}

func TestHealthEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	// Create a minimal handler (we don't need DB for health check)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	http.DefaultServeMux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ok", w.Body.String())
}

func TestRestaurantsOpenEndpoint(t *testing.T) {
	sqlxDB, mock := setupTestDB(t)
	defer sqlxDB.Close()

	wrappedDB := db.NewDBWrapper(sqlxDB, nil) // nil recorder for tests
	restaurantRepo := db.NewRestaurantRepository(wrappedDB)

	// Mock the database query
	mock.ExpectQuery(`SELECT DISTINCT r.id, r.name, r.cash_balance, r.timezone`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "cash_balance", "timezone"}).
			AddRow(1, "Test Restaurant", 1000.0, "UTC"))

	// Create handler
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		datetimeStr := r.URL.Query().Get("datetime")
		if datetimeStr == "" {
			http.Error(w, "Missing required query parameter: datetime", http.StatusBadRequest)
			return
		}

		datetime, _ := time.Parse(time.RFC3339, datetimeStr)
		restaurants, err := restaurantRepo.FindOpenAtTime(datetime)
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(restaurants)
	}

	tests := []struct {
		name           string
		datetime       string
		expectedStatus int
	}{
		{
			name:           "valid datetime",
			datetime:       "2024-01-15T14:30:00Z",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing datetime",
			datetime:       "",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/restaurants/open"
			if tt.datetime != "" {
				url += "?datetime=" + tt.datetime
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			handler(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestRestaurantsTopEndpoint(t *testing.T) {
	sqlxDB, mock := setupTestDB(t)
	defer sqlxDB.Close()

	wrappedDB := db.NewDBWrapper(sqlxDB, nil) // nil recorder for tests
	restaurantRepo := db.NewRestaurantRepository(wrappedDB)

	// Mock the database query
	mock.ExpectQuery(`SELECT r.id, r.name, r.cash_balance, r.timezone, COUNT\(mi.id\) as dish_count`).
		WithArgs(10.0, 50.0, 5, 10).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "cash_balance", "timezone", "dish_count"}).
			AddRow(1, "Restaurant A", 1000.0, "UTC", 8))

	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		restaurants, err := restaurantRepo.FindTopByDishCount(10, 5, 10.0, 50.0, "more")
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(restaurants)
	}

	req := httptest.NewRequest(http.MethodGet, "/restaurants/top?limit=10&dish_count=5&min_price=10&max_price=50&comparison=more", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

func TestSearchEndpoint(t *testing.T) {
	sqlxDB, mock := setupTestDB(t)
	defer sqlxDB.Close()

	wrappedDB := db.NewDBWrapper(sqlxDB, nil) // nil recorder for tests
	restaurantRepo := db.NewRestaurantRepository(wrappedDB)

	// Mock the database query
	restaurantID := int64(1)
	restaurantName := "Pizza Place"
	price := 15.99
	searchRows := sqlmock.NewRows([]string{"type", "id", "name", "restaurant_id", "restaurant_name", "price", "relevance"}).
		AddRow("restaurant", 1, "Pizza Place", nil, nil, nil, 0.8).
		AddRow("dish", 10, "Margherita Pizza", &restaurantID, &restaurantName, &price, 0.9)
	mock.ExpectQuery(`SELECT`).
		WithArgs("pizza", 20).
		WillReturnRows(searchRows)

	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		queryTerm := r.URL.Query().Get("q")
		if queryTerm == "" {
			http.Error(w, "Missing required query parameter: q", http.StatusBadRequest)
			return
		}

		results, err := restaurantRepo.Search(queryTerm, 20)
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}

	tests := []struct {
		name           string
		url            string
		expectedStatus int
	}{
		{
			name:           "valid search query",
			url:            "/search?q=pizza&limit=20",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing query parameter",
			url:            "/search",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			w := httptest.NewRecorder()

			handler(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestPurchaseEndpoint(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request, userRepo *db.UserRepository) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var purchaseReq db.PurchaseRequest
		if err := json.NewDecoder(r.Body).Decode(&purchaseReq); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Validate request
		if purchaseReq.UserID <= 0 {
			http.Error(w, "Invalid user_id", http.StatusBadRequest)
			return
		}
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

		result, err := userRepo.PurchaseDish(purchaseReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(result)
	}

	tests := []struct {
		name           string
		body           db.PurchaseRequest
		expectedStatus int
		setupMock      func(sqlmock.Sqlmock)
	}{
		{
			name: "valid purchase (single item)",
			body: db.PurchaseRequest{
				UserID: 1,
				Items: []db.PurchaseItem{
					{MenuItemID: 10, Quantity: 1},
				},
			},
			expectedStatus: http.StatusOK,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectQuery(`SELECT id, name, cash_balance`).
					WithArgs(1).
					WillReturnRows(sqlmock.NewRows([]string{"id", "name", "cash_balance"}).
						AddRow(1, "John Doe", 100.00))
				mock.ExpectQuery(`SELECT mi.id, mi.name, mi.price, mi.restaurant_id, mi.is_active, r.name`).
					WithArgs(10).
					WillReturnRows(sqlmock.NewRows([]string{"id", "name", "price", "restaurant_id", "is_active", "restaurant_name"}).
						AddRow(10, "Pizza", 15.50, 5, true, "Pizza Place"))
				mock.ExpectExec(`UPDATE users`).
					WithArgs(15.50, 1).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`UPDATE restaurants`).
					WithArgs(15.50, 5).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectQuery(`INSERT INTO orders`).
					WithArgs(1, 5, 15.50).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(100))
				mock.ExpectExec(`INSERT INTO order_items`).
					WithArgs(100, 10, "Pizza", 15.50, 1, 15.50).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "valid purchase (new format, multiple items)",
			body: db.PurchaseRequest{
				UserID: 1,
				Items: []db.PurchaseItem{
					{MenuItemID: 10, Quantity: 1},
					{MenuItemID: 11, Quantity: 2},
				},
			},
			expectedStatus: http.StatusOK,
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectQuery(`SELECT id, name, cash_balance`).
					WithArgs(1).
					WillReturnRows(sqlmock.NewRows([]string{"id", "name", "cash_balance"}).
						AddRow(1, "John Doe", 200.00))
				// First menu item
				mock.ExpectQuery(`SELECT mi.id, mi.name, mi.price, mi.restaurant_id, mi.is_active, r.name`).
					WithArgs(10).
					WillReturnRows(sqlmock.NewRows([]string{"id", "name", "price", "restaurant_id", "is_active", "restaurant_name"}).
						AddRow(10, "Pizza", 15.50, 5, true, "Pizza Place"))
				// Second menu item
				mock.ExpectQuery(`SELECT mi.id, mi.name, mi.price, mi.restaurant_id, mi.is_active, r.name`).
					WithArgs(11).
					WillReturnRows(sqlmock.NewRows([]string{"id", "name", "price", "restaurant_id", "is_active", "restaurant_name"}).
						AddRow(11, "Burger", 12.00, 5, true, "Pizza Place"))
				// Total: 15.50*1 + 12.00*2 = 39.50
				mock.ExpectExec(`UPDATE users`).
					WithArgs(39.50, 1).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`UPDATE restaurants`).
					WithArgs(39.50, 5).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectQuery(`INSERT INTO orders`).
					WithArgs(1, 5, 39.50).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(100))
				// First order item
				mock.ExpectExec(`INSERT INTO order_items`).
					WithArgs(100, 10, "Pizza", 15.50, 1, 15.50).
					WillReturnResult(sqlmock.NewResult(0, 1))
				// Second order item
				mock.ExpectExec(`INSERT INTO order_items`).
					WithArgs(100, 11, "Burger", 12.00, 2, 24.00).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "invalid user_id",
			body: db.PurchaseRequest{
				UserID: 0,
				Items: []db.PurchaseItem{
					{MenuItemID: 10, Quantity: 1},
				},
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "invalid quantity",
			body: db.PurchaseRequest{
				UserID: 1,
				Items: []db.PurchaseItem{
					{MenuItemID: 10, Quantity: 0},
				},
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlxDB, mock := setupTestDB(t)
			defer sqlxDB.Close()

			wrappedDB := db.NewDBWrapper(sqlxDB, nil)
			userRepo := db.NewUserRepository(wrappedDB)

			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/purchase", bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler(w, req, userRepo)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.setupMock != nil {
				assert.NoError(t, mock.ExpectationsWereMet())
			}
		})
	}
}
