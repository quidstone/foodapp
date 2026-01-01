package db

import (
	"database/sql"
	"errors"
	"fmt"
)

// User represents a user in the database
type User struct {
	ID          int64   `db:"id" json:"id"`
	Name        string  `db:"name" json:"name"`
	CashBalance float64 `db:"cash_balance" json:"cash_balance"`
}

// PurchaseItem represents a single item in a purchase request
type PurchaseItem struct {
	MenuItemID int64 `json:"menu_item_id"`
	Quantity   int   `json:"quantity"`
}

// PurchaseRequest represents a purchase request with multiple items
type PurchaseRequest struct {
	UserID         int64          `json:"user_id"`
	Items          []PurchaseItem `json:"items"`
	IdempotencyKey string         `json:"-"` // Set from header, not JSON body
}

// PurchaseResult represents the result of a purchase
type PurchaseResult struct {
	OrderID      int64   `db:"order_id" json:"order_id"`
	UserID       int64   `db:"user_id" json:"user_id"`
	RestaurantID int64   `db:"restaurant_id" json:"restaurant_id"`
	TotalAmount  float64 `db:"total_amount" json:"total_amount"`
	Message      string  `db:"message" json:"message"`
}

// UserRepository handles user-related database queries
type UserRepository struct {
	db *DBWrapper
}

// NewUserRepository creates a new user repository
func NewUserRepository(db *DBWrapper) *UserRepository {
	return &UserRepository{db: db}
}

// PurchaseDish processes a user purchasing dishes from a restaurant
// All items must be from the same restaurant
// Uses FOR UPDATE row-level locking to prevent race conditions
// Supports idempotency keys to prevent duplicate orders on retry
// Returns error if user has insufficient balance, dish not found, or transaction fails
func (r *UserRepository) PurchaseDish(req PurchaseRequest) (*PurchaseResult, error) {
	// Validate items
	if len(req.Items) == 0 {
		return nil, errors.New("items list cannot be empty")
	}
	for i, item := range req.Items {
		if item.MenuItemID <= 0 {
			return nil, fmt.Errorf("invalid menu_item_id at index %d", i)
		}
		if item.Quantity <= 0 {
			return nil, fmt.Errorf("invalid quantity at index %d (must be > 0)", i)
		}
	}

	items := req.Items

	// Check for existing idempotency key (before starting transaction)
	if req.IdempotencyKey != "" {
		var existingResponse PurchaseResult
		err := r.db.Get(&existingResponse, `
			SELECT
				(response->>'order_id')::BIGINT as order_id,
				(response->>'user_id')::BIGINT as user_id,
				(response->>'restaurant_id')::BIGINT as restaurant_id,
				(response->>'total_amount')::NUMERIC as total_amount,
				response->>'message' as message
			FROM purchase_idempotency_keys
			WHERE key = $1
		`, req.IdempotencyKey)
		if err == nil {
			// Found existing response, return it
			return &existingResponse, nil
		}
		// If error is not "no rows", something went wrong
		if err != sql.ErrNoRows {
			return nil, fmt.Errorf("failed to check idempotency key: %w", err)
		}
		// No existing key found, proceed with purchase
	}

	// Start transaction
	tx, err := r.db.Beginx()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Lock user row for update to prevent concurrent balance modifications
	var user User
	err = tx.Get(&user, `
		SELECT id, name, cash_balance 
		FROM users 
		WHERE id = $1 
		FOR UPDATE
	`, req.UserID)
	if err == sql.ErrNoRows {
		return nil, errors.New("user not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Menu item structure
	type menuItemInfo struct {
		ID             int64   `db:"id"`
		Name           string  `db:"name"`
		Price          float64 `db:"price"`
		RestaurantID   int64   `db:"restaurant_id"`
		IsActive       bool    `db:"is_active"`
		RestaurantName string  `db:"restaurant_name"`
	}

	// Fetch and validate all menu items
	menuItems := make([]menuItemInfo, len(items))
	var restaurantID int64
	var restaurantName string

	for i, item := range items {
		var mi menuItemInfo
		err = tx.Get(&mi, `
			SELECT mi.id, mi.name, mi.price, mi.restaurant_id, mi.is_active, r.name as restaurant_name
			FROM menu_items mi
			INNER JOIN restaurants r ON r.id = mi.restaurant_id
			WHERE mi.id = $1
			FOR UPDATE OF mi, r
		`, item.MenuItemID)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("menu item not found: menu_item_id %d", item.MenuItemID)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to get menu item %d: %w", item.MenuItemID, err)
		}

		// Validate menu item is active
		if !mi.IsActive {
			return nil, fmt.Errorf("menu item is not active: menu_item_id %d", item.MenuItemID)
		}

		// Ensure all items are from the same restaurant
		if i == 0 {
			restaurantID = mi.RestaurantID
			restaurantName = mi.RestaurantName
		} else if mi.RestaurantID != restaurantID {
			return nil, fmt.Errorf("all items must be from the same restaurant: item at index %d is from a different restaurant", i)
		}

		menuItems[i] = mi
	}

	// Calculate total amount across all items
	var totalAmount float64
	var itemMessages []string
	for i, item := range items {
		lineAmount := menuItems[i].Price * float64(item.Quantity)
		totalAmount += lineAmount
		itemMessages = append(itemMessages, fmt.Sprintf("%d x %s", item.Quantity, menuItems[i].Name))
	}

	// Validate user has sufficient balance
	if user.CashBalance < totalAmount {
		return nil, fmt.Errorf("insufficient balance: user has %.2f, need %.2f", user.CashBalance, totalAmount)
	}

	// Update user balance (decrease)
	_, err = tx.Exec(`
		UPDATE users 
		SET cash_balance = cash_balance - $1 
		WHERE id = $2
	`, totalAmount, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to update user balance: %w", err)
	}

	// Update restaurant balance (increase)
	_, err = tx.Exec(`
		UPDATE restaurants 
		SET cash_balance = cash_balance + $1 
		WHERE id = $2
	`, totalAmount, restaurantID)
	if err != nil {
		return nil, fmt.Errorf("failed to update restaurant balance: %w", err)
	}

	// Create order
	var orderID int64
	err = tx.Get(&orderID, `
		INSERT INTO orders (user_id, restaurant_id, total_amount)
		VALUES ($1, $2, $3)
		RETURNING id
	`, req.UserID, restaurantID, totalAmount)
	if err != nil {
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	// Create order items for each purchased item
	for i, item := range items {
		lineAmount := menuItems[i].Price * float64(item.Quantity)
		_, err = tx.Exec(`
			INSERT INTO order_items (order_id, menu_item_id, dish_name, unit_price, quantity, line_amount)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, orderID, item.MenuItemID, menuItems[i].Name, menuItems[i].Price, item.Quantity, lineAmount)
		if err != nil {
			return nil, fmt.Errorf("failed to create order item for menu_item_id %d: %w", item.MenuItemID, err)
		}
	}

	// Build result message
	message := fmt.Sprintf("Successfully purchased %s from %s",
		fmt.Sprintf("%d item(s)", len(items)), restaurantName)
	if len(items) == 1 {
		message = fmt.Sprintf("Successfully purchased %s from %s", itemMessages[0], restaurantName)
	}

	result := &PurchaseResult{
		OrderID:      orderID,
		UserID:       req.UserID,
		RestaurantID: restaurantID,
		TotalAmount:  totalAmount,
		Message:      message,
	}

	// Store idempotency key with response (within same transaction)
	if req.IdempotencyKey != "" {
		_, err = tx.Exec(`
			INSERT INTO purchase_idempotency_keys (key, user_id, response)
			VALUES ($1, $2, $3)
		`, req.IdempotencyKey, req.UserID, fmt.Sprintf(
			`{"order_id":%d,"user_id":%d,"restaurant_id":%d,"total_amount":%f,"message":"%s"}`,
			result.OrderID, result.UserID, result.RestaurantID, result.TotalAmount, result.Message,
		))
		if err != nil {
			return nil, fmt.Errorf("failed to store idempotency key: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return result, nil
}
