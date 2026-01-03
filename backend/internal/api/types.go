package api

import "github.com/shopspring/decimal"

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

// PurchaseResult represents the result of a purchase sent back to the client
type PurchaseResult struct {
	OrderID      int64           `json:"order_id"`
	UserID       int64           `json:"user_id"`
	RestaurantID int64           `json:"restaurant_id"`
	TotalAmount  decimal.Decimal `json:"total_amount"`
	Message      string          `json:"message"`
}
