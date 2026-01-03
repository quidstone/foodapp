package db

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestUserRepository_PurchaseDish(t *testing.T) {
	tests := []struct {
		name        string
		request     PurchaseParams
		setupMock   func(mock sqlmock.Sqlmock)
		expectError bool
		errorMsg    string
	}{
		{
			name: "success - valid purchase (single item)",
			request: PurchaseParams{
				UserID: 1,
				Items: []PurchaseItem{
					{MenuItemID: 10, Quantity: 2},
				},
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				// Begin transaction
				mock.ExpectBegin()

				// Get user with FOR UPDATE
				rows := sqlmock.NewRows([]string{"id", "name", "cash_balance"}).
					AddRow(1, "John Doe", 100.00)
				mock.ExpectQuery(`SELECT id, name, cash_balance`).
					WithArgs(1).
					WillReturnRows(rows)

				// Get menu item with FOR UPDATE
				menuRows := sqlmock.NewRows([]string{"id", "name", "price", "restaurant_id", "is_active", "restaurant_name"}).
					AddRow(10, "Pizza", 15.50, 5, true, "Pizza Place")
				mock.ExpectQuery(`SELECT mi.id, mi.name, mi.price, mi.restaurant_id, mi.is_active, r.name`).
					WithArgs(10).
					WillReturnRows(menuRows)

				// Update user balance
				mock.ExpectExec(`UPDATE users`).
					WithArgs(decimal.NewFromFloat(31.00), 1). // totalAmount = 15.50 * 2
					WillReturnResult(sqlmock.NewResult(0, 1))

				// Update restaurant balance
				mock.ExpectExec(`UPDATE restaurants`).
					WithArgs(decimal.NewFromFloat(31.00), 5).
					WillReturnResult(sqlmock.NewResult(0, 1))

				// Create order
				mock.ExpectQuery(`INSERT INTO orders`).
					WithArgs(1, 5, decimal.NewFromFloat(31.00)).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(100))

				// Create order item
				mock.ExpectExec(`INSERT INTO order_items`).
					WithArgs(100, 10, "Pizza", decimal.NewFromFloat(15.50), 2, decimal.NewFromFloat(31.00)).
					WillReturnResult(sqlmock.NewResult(0, 1))

				// Commit transaction
				mock.ExpectCommit()
			},
			expectError: false,
		},
		{
			name: "success - valid purchase (multiple items, new format)",
			request: PurchaseParams{
				UserID: 1,
				Items: []PurchaseItem{
					{MenuItemID: 10, Quantity: 2},
					{MenuItemID: 11, Quantity: 1},
				},
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				// Begin transaction
				mock.ExpectBegin()

				// Get user with FOR UPDATE
				rows := sqlmock.NewRows([]string{"id", "name", "cash_balance"}).
					AddRow(1, "John Doe", 200.00)
				mock.ExpectQuery(`SELECT id, name, cash_balance`).
					WithArgs(1).
					WillReturnRows(rows)

				// Get first menu item (Pizza)
				menuRows1 := sqlmock.NewRows([]string{"id", "name", "price", "restaurant_id", "is_active", "restaurant_name"}).
					AddRow(10, "Pizza", 15.50, 5, true, "Pizza Place")
				mock.ExpectQuery(`SELECT mi.id, mi.name, mi.price, mi.restaurant_id, mi.is_active, r.name`).
					WithArgs(10).
					WillReturnRows(menuRows1)

				// Get second menu item (Burger) - same restaurant
				menuRows2 := sqlmock.NewRows([]string{"id", "name", "price", "restaurant_id", "is_active", "restaurant_name"}).
					AddRow(11, "Burger", 12.00, 5, true, "Pizza Place")
				mock.ExpectQuery(`SELECT mi.id, mi.name, mi.price, mi.restaurant_id, mi.is_active, r.name`).
					WithArgs(11).
					WillReturnRows(menuRows2)

				// Update user balance (total: 15.50*2 + 12.00*1 = 43.00)
				mock.ExpectExec(`UPDATE users`).
					WithArgs(decimal.NewFromFloat(43.00), 1).
					WillReturnResult(sqlmock.NewResult(0, 1))

				// Update restaurant balance
				mock.ExpectExec(`UPDATE restaurants`).
					WithArgs(decimal.NewFromFloat(43.00), 5).
					WillReturnResult(sqlmock.NewResult(0, 1))

				// Create order
				mock.ExpectQuery(`INSERT INTO orders`).
					WithArgs(1, 5, decimal.NewFromFloat(43.00)).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(100))

				// Create first order item
				mock.ExpectExec(`INSERT INTO order_items`).
					WithArgs(100, 10, "Pizza", decimal.NewFromFloat(15.50), 2, decimal.NewFromFloat(31.00)).
					WillReturnResult(sqlmock.NewResult(0, 1))

				// Create second order item
				mock.ExpectExec(`INSERT INTO order_items`).
					WithArgs(100, 11, "Burger", decimal.NewFromFloat(12.00), 1, decimal.NewFromFloat(12.00)).
					WillReturnResult(sqlmock.NewResult(0, 1))

				// Commit transaction
				mock.ExpectCommit()
			},
			expectError: false,
		},
		{
			name: "error - user not found",
			request: PurchaseParams{
				UserID: 999,
				Items: []PurchaseItem{
					{MenuItemID: 10, Quantity: 1},
				},
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectQuery(`SELECT id, name, cash_balance`).
					WithArgs(999).
					WillReturnError(sql.ErrNoRows)
				mock.ExpectRollback()
			},
			expectError: true,
			errorMsg:    "user not found",
		},
		{
			name: "error - menu item not found",
			request: PurchaseParams{
				UserID: 1,
				Items: []PurchaseItem{
					{MenuItemID: 999, Quantity: 1},
				},
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				userRows := sqlmock.NewRows([]string{"id", "name", "cash_balance"}).
					AddRow(1, "John Doe", 100.00)
				mock.ExpectQuery(`SELECT id, name, cash_balance`).
					WithArgs(1).
					WillReturnRows(userRows)
				mock.ExpectQuery(`SELECT mi.id, mi.name, mi.price, mi.restaurant_id, mi.is_active, r.name`).
					WithArgs(999).
					WillReturnError(sql.ErrNoRows)
				mock.ExpectRollback()
			},
			expectError: true,
			errorMsg:    "menu item not found",
		},
		{
			name: "error - items from different restaurants",
			request: PurchaseParams{
				UserID: 1,
				Items: []PurchaseItem{
					{MenuItemID: 10, Quantity: 1}, // Restaurant 5
					{MenuItemID: 20, Quantity: 1}, // Restaurant 6
				},
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				userRows := sqlmock.NewRows([]string{"id", "name", "cash_balance"}).
					AddRow(1, "John Doe", 100.00)
				mock.ExpectQuery(`SELECT id, name, cash_balance`).
					WithArgs(1).
					WillReturnRows(userRows)
				// First item from restaurant 5
				menuRows1 := sqlmock.NewRows([]string{"id", "name", "price", "restaurant_id", "is_active", "restaurant_name"}).
					AddRow(10, "Pizza", 15.50, 5, true, "Pizza Place")
				mock.ExpectQuery(`SELECT mi.id, mi.name, mi.price, mi.restaurant_id, mi.is_active, r.name`).
					WithArgs(10).
					WillReturnRows(menuRows1)
				// Second item from different restaurant 6
				menuRows2 := sqlmock.NewRows([]string{"id", "name", "price", "restaurant_id", "is_active", "restaurant_name"}).
					AddRow(20, "Burger", 12.00, 6, true, "Burger Joint")
				mock.ExpectQuery(`SELECT mi.id, mi.name, mi.price, mi.restaurant_id, mi.is_active, r.name`).
					WithArgs(20).
					WillReturnRows(menuRows2)
				mock.ExpectRollback()
			},
			expectError: true,
			errorMsg:    "all items must be from the same restaurant",
		},
		{
			name: "error - menu item not active",
			request: PurchaseParams{
				UserID: 1,
				Items: []PurchaseItem{
					{MenuItemID: 10, Quantity: 1},
				},
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				userRows := sqlmock.NewRows([]string{"id", "name", "cash_balance"}).
					AddRow(1, "John Doe", 100.00)
				mock.ExpectQuery(`SELECT id, name, cash_balance`).
					WithArgs(1).
					WillReturnRows(userRows)
				menuRows := sqlmock.NewRows([]string{"id", "name", "price", "restaurant_id", "is_active", "restaurant_name"}).
					AddRow(10, "Pizza", 15.50, 5, false, "Pizza Place")
				mock.ExpectQuery(`SELECT mi.id, mi.name, mi.price, mi.restaurant_id, mi.is_active, r.name`).
					WithArgs(10).
					WillReturnRows(menuRows)
				mock.ExpectRollback()
			},
			expectError: true,
			errorMsg:    "menu item is not active",
		},
		{
			name: "error - insufficient balance (single item)",
			request: PurchaseParams{
				UserID: 1,
				Items: []PurchaseItem{
					{MenuItemID: 10, Quantity: 10}, // User has 100, item costs 15.50, needs 155
				},
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				userRows := sqlmock.NewRows([]string{"id", "name", "cash_balance"}).
					AddRow(1, "John Doe", 100.00)
				mock.ExpectQuery(`SELECT id, name, cash_balance`).
					WithArgs(1).
					WillReturnRows(userRows)
				menuRows := sqlmock.NewRows([]string{"id", "name", "price", "restaurant_id", "is_active", "restaurant_name"}).
					AddRow(10, "Pizza", 15.50, 5, true, "Pizza Place")
				mock.ExpectQuery(`SELECT mi.id, mi.name, mi.price, mi.restaurant_id, mi.is_active, r.name`).
					WithArgs(10).
					WillReturnRows(menuRows)
				mock.ExpectRollback()
			},
			expectError: true,
			errorMsg:    "insufficient balance",
		},
		{
			name: "error - insufficient balance (multiple items)",
			request: PurchaseParams{
				UserID: 1,
				Items: []PurchaseItem{
					{MenuItemID: 10, Quantity: 5}, // 15.50 * 5 = 77.50
					{MenuItemID: 11, Quantity: 2}, // 12.00 * 2 = 24.00, total = 101.50
				},
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				userRows := sqlmock.NewRows([]string{"id", "name", "cash_balance"}).
					AddRow(1, "John Doe", 100.00) // Only 100.00, needs 101.50
				mock.ExpectQuery(`SELECT id, name, cash_balance`).
					WithArgs(1).
					WillReturnRows(userRows)
				menuRows1 := sqlmock.NewRows([]string{"id", "name", "price", "restaurant_id", "is_active", "restaurant_name"}).
					AddRow(10, "Pizza", 15.50, 5, true, "Pizza Place")
				mock.ExpectQuery(`SELECT mi.id, mi.name, mi.price, mi.restaurant_id, mi.is_active, r.name`).
					WithArgs(10).
					WillReturnRows(menuRows1)
				menuRows2 := sqlmock.NewRows([]string{"id", "name", "price", "restaurant_id", "is_active", "restaurant_name"}).
					AddRow(11, "Burger", 12.00, 5, true, "Pizza Place")
				mock.ExpectQuery(`SELECT mi.id, mi.name, mi.price, mi.restaurant_id, mi.is_active, r.name`).
					WithArgs(11).
					WillReturnRows(menuRows2)
				mock.ExpectRollback()
			},
			expectError: true,
			errorMsg:    "insufficient balance",
		},
		{
			name: "success - with idempotency key (first request)",
			request: PurchaseParams{
				UserID: 1,
				Items: []PurchaseItem{
					{MenuItemID: 10, Quantity: 2},
				},
				IdempotencyKey: "test-idempotency-key-123",
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				// Check for existing idempotency key - not found
				mock.ExpectQuery(`SELECT`).
					WithArgs("test-idempotency-key-123").
					WillReturnError(sql.ErrNoRows)

				// Begin transaction
				mock.ExpectBegin()

				// Get user with FOR UPDATE
				rows := sqlmock.NewRows([]string{"id", "name", "cash_balance"}).
					AddRow(1, "John Doe", 100.00)
				mock.ExpectQuery(`SELECT id, name, cash_balance`).
					WithArgs(1).
					WillReturnRows(rows)

				// Get menu item with FOR UPDATE
				menuRows := sqlmock.NewRows([]string{"id", "name", "price", "restaurant_id", "is_active", "restaurant_name"}).
					AddRow(10, "Pizza", 15.50, 5, true, "Pizza Place")
				mock.ExpectQuery(`SELECT mi.id, mi.name, mi.price, mi.restaurant_id, mi.is_active, r.name`).
					WithArgs(10).
					WillReturnRows(menuRows)

				// Update user balance
				mock.ExpectExec(`UPDATE users`).
					WithArgs(decimal.NewFromFloat(31.00), 1).
					WillReturnResult(sqlmock.NewResult(0, 1))

				// Update restaurant balance
				mock.ExpectExec(`UPDATE restaurants`).
					WithArgs(decimal.NewFromFloat(31.00), 5).
					WillReturnResult(sqlmock.NewResult(0, 1))

				// Create order
				mock.ExpectQuery(`INSERT INTO orders`).
					WithArgs(1, 5, decimal.NewFromFloat(31.00)).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(100))

				// Create order item
				mock.ExpectExec(`INSERT INTO order_items`).
					WithArgs(100, 10, "Pizza", decimal.NewFromFloat(15.50), 2, decimal.NewFromFloat(31.00)).
					WillReturnResult(sqlmock.NewResult(0, 1))

				// Insert idempotency key
				mock.ExpectExec(`INSERT INTO purchase_idempotency_keys`).
					WithArgs("test-idempotency-key-123", int64(1), sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(0, 1))

				// Commit transaction
				mock.ExpectCommit()
			},
			expectError: false,
		},
		{
			name: "success - duplicate idempotency key returns cached response",
			request: PurchaseParams{
				UserID: 1,
				Items: []PurchaseItem{
					{MenuItemID: 10, Quantity: 2},
				},
				IdempotencyKey: "existing-key-456",
			},
			setupMock: func(mock sqlmock.Sqlmock) {
				// Check for existing idempotency key - found!
				cachedRows := sqlmock.NewRows([]string{"order_id", "user_id", "restaurant_id", "total_amount", "message"}).
					AddRow(42, 1, 5, decimal.NewFromFloat(31.00), "Successfully purchased 2 x Pizza from Pizza Place")
				mock.ExpectQuery(`SELECT`).
					WithArgs("existing-key-456").
					WillReturnRows(cachedRows)

				// No transaction should be started - we return cached response
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create mock: %v", err)
			}
			defer db.Close()

			sqlxDB := sqlx.NewDb(db, "postgres")
			wrappedDB := NewDBWrapper(sqlxDB, nil) // nil recorder for tests
			repo := NewUserRepository(wrappedDB)

			tt.setupMock(mock)

			result, err := repo.PurchaseDish(tt.request)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Greater(t, result.OrderID, int64(0))
				assert.Equal(t, tt.request.UserID, result.UserID)
				assert.True(t, result.TotalAmount.GreaterThan(decimal.Zero))
				assert.NotEmpty(t, result.Message)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
