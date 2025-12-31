package db

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// User represents a user in the database
type User struct {
	ID          int64   `db:"id" json:"id"`
	Name        string  `db:"name" json:"name"`
	CashBalance float64 `db:"cash_balance" json:"cash_balance"`
}

// PurchaseRequest represents a purchase request
type PurchaseRequest struct {
	UserID     int64 `json:"user_id"`
	MenuItemID int64 `json:"menu_item_id"`
	Quantity   int   `json:"quantity"`
}

// PurchaseResult represents the result of a purchase
type PurchaseResult struct {
	OrderID      int64   `json:"order_id"`
	UserID       int64   `json:"user_id"`
	RestaurantID int64   `json:"restaurant_id"`
	TotalAmount  float64 `json:"total_amount"`
	Message      string  `json:"message"`
}

// UserRepository handles user-related database queries
type UserRepository struct {
	db *sqlx.DB
}

// NewUserRepository creates a new user repository
func NewUserRepository(db *sqlx.DB) *UserRepository {
	return &UserRepository{db: db}
}

// PurchaseDish processes a user purchasing a dish from a restaurant
// Uses FOR UPDATE row-level locking to prevent race conditions
// Returns error if user has insufficient balance, dish not found, or transaction fails
func (r *UserRepository) PurchaseDish(req PurchaseRequest) (*PurchaseResult, error) {
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

	// Get menu item with restaurant info, lock restaurant row
	var menuItem struct {
		ID             int64   `db:"id"`
		Name           string  `db:"name"`
		Price          float64 `db:"price"`
		RestaurantID   int64   `db:"restaurant_id"`
		IsActive       bool    `db:"is_active"`
		RestaurantName string  `db:"restaurant_name"`
	}
	err = tx.Get(&menuItem, `
		SELECT mi.id, mi.name, mi.price, mi.restaurant_id, mi.is_active, r.name as restaurant_name
		FROM menu_items mi
		INNER JOIN restaurants r ON r.id = mi.restaurant_id
		WHERE mi.id = $1
		FOR UPDATE OF r
	`, req.MenuItemID)
	if err == sql.ErrNoRows {
		return nil, errors.New("menu item not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get menu item: %w", err)
	}

	// Validate menu item is active
	if !menuItem.IsActive {
		return nil, errors.New("menu item is not active")
	}

	// Calculate total amount
	totalAmount := menuItem.Price * float64(req.Quantity)

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
	`, totalAmount, menuItem.RestaurantID)
	if err != nil {
		return nil, fmt.Errorf("failed to update restaurant balance: %w", err)
	}

	// Create order
	var orderID int64
	err = tx.Get(&orderID, `
		INSERT INTO orders (user_id, restaurant_id, total_amount)
		VALUES ($1, $2, $3)
		RETURNING id
	`, req.UserID, menuItem.RestaurantID, totalAmount)
	if err != nil {
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	// Create order item
	_, err = tx.Exec(`
		INSERT INTO order_items (order_id, menu_item_id, dish_name, unit_price, quantity, line_amount)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, orderID, req.MenuItemID, menuItem.Name, menuItem.Price, req.Quantity, totalAmount)
	if err != nil {
		return nil, fmt.Errorf("failed to create order item: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &PurchaseResult{
		OrderID:      orderID,
		UserID:       req.UserID,
		RestaurantID: menuItem.RestaurantID,
		TotalAmount:  totalAmount,
		Message:      fmt.Sprintf("Successfully purchased %d x %s from %s", req.Quantity, menuItem.Name, menuItem.RestaurantName),
	}, nil
}
