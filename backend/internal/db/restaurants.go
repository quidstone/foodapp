package db

import (
	"time"

	"github.com/jmoiron/sqlx"
)

// Restaurant represents a restaurant in the database
type Restaurant struct {
	ID          int64   `db:"id" json:"id"`
	Name        string  `db:"name" json:"name"`
	CashBalance float64 `db:"cash_balance" json:"cash_balance"`
	Timezone    string  `db:"timezone" json:"timezone"`
}

// RestaurantRepository handles restaurant-related database queries
type RestaurantRepository struct {
	db *sqlx.DB
}

// NewRestaurantRepository creates a new restaurant repository
func NewRestaurantRepository(db *sqlx.DB) *RestaurantRepository {
	return &RestaurantRepository{db: db}
}

// FindOpenAtTime returns all restaurants that are open at the given datetime
// The datetime is checked against each restaurant's timezone and opening hours
func (r *RestaurantRepository) FindOpenAtTime(datetime time.Time) ([]Restaurant, error) {
	var restaurants []Restaurant

	// Query to find restaurants open at the given datetime
	// We convert the input datetime to each restaurant's timezone,
	// extract the day of week and time, and check against opening_hours
	query := `
		SELECT DISTINCT r.id, r.name, r.cash_balance, r.timezone
		FROM restaurants r
		WHERE EXISTS (
			SELECT 1
			FROM restaurant_opening_hours roh
			WHERE roh.restaurant_id = r.id
			AND roh.day_of_week = EXTRACT(DOW FROM $1 AT TIME ZONE r.timezone)
			AND (
				-- Normal case: open_time <= close_time (same day)
				(roh.open_time <= roh.close_time 
				 AND (($1 AT TIME ZONE r.timezone)::TIME >= roh.open_time 
				      AND ($1 AT TIME ZONE r.timezone)::TIME < roh.close_time))
				OR
				-- Overnight case: close_time < open_time (spans midnight)
				(roh.open_time > roh.close_time 
				 AND (($1 AT TIME ZONE r.timezone)::TIME >= roh.open_time 
				      OR ($1 AT TIME ZONE r.timezone)::TIME < roh.close_time))
			)
		)
		ORDER BY r.name
	`

	err := r.db.Select(&restaurants, query, datetime)
	if err != nil {
		return nil, err
	}

	return restaurants, nil
}

// RestaurantWithDishCount represents a restaurant with its dish count
type RestaurantWithDishCount struct {
	Restaurant
	DishCount int `db:"dish_count" json:"dish_count"`
}

// FindTopByDishCount returns top y restaurants that have more or less than x dishes
// within the specified price range, ranked alphabetically
// comparison: "more" for > x, "less" for < x
func (r *RestaurantRepository) FindTopByDishCount(limit int, dishCountThreshold int, minPrice, maxPrice float64, comparison string) ([]RestaurantWithDishCount, error) {
	var restaurants []RestaurantWithDishCount

	// Build comparison operator
	var op string
	switch comparison {
	case "more", "greater", "gt", ">":
		op = ">"
	case "less", "fewer", "lt", "<":
		op = "<"
	default:
		op = ">" // default to "more"
	}

	// Query: Count dishes within price range per restaurant,
	// filter by dish count threshold, limit and order alphabetically
	query := `
		SELECT r.id, r.name, r.cash_balance, r.timezone, COUNT(mi.id) as dish_count
		FROM restaurants r
		INNER JOIN menu_items mi ON mi.restaurant_id = r.id
		WHERE mi.is_active = TRUE
		AND mi.price >= $1 AND mi.price <= $2
		GROUP BY r.id, r.name, r.cash_balance, r.timezone
		HAVING COUNT(mi.id) ` + op + ` $3
		ORDER BY r.name
		LIMIT $4
	`

	err := r.db.Select(&restaurants, query, minPrice, maxPrice, dishCountThreshold, limit)
	if err != nil {
		return nil, err
	}

	return restaurants, nil
}

// SearchResult represents a search result (restaurant or dish)
type SearchResult struct {
	Type           string   `db:"type" json:"type"` // "restaurant" or "dish"
	ID             int64    `db:"id" json:"id"`
	Name           string   `db:"name" json:"name"`
	RestaurantID   *int64   `db:"restaurant_id" json:"restaurant_id,omitempty"`
	RestaurantName *string  `db:"restaurant_name" json:"restaurant_name,omitempty"`
	Price          *float64 `db:"price" json:"price,omitempty"`
	Relevance      float64  `db:"relevance" json:"relevance"`
}

// Search searches for restaurants and dishes by name using full-text search
// Returns results ranked by relevance
func (r *RestaurantRepository) Search(queryTerm string, limit int) ([]SearchResult, error) {
	var results []SearchResult

	// Full-text search query using PostgreSQL's ts_rank for relevance
	// Searches both restaurants and menu_items
	sqlQuery := `
		(
			SELECT 
				'restaurant' as type,
				r.id,
				r.name,
				NULL::BIGINT as restaurant_id,
				NULL::TEXT as restaurant_name,
				NULL::NUMERIC as price,
				ts_rank(r.name_search, plainto_tsquery('english', $1)) as relevance
			FROM restaurants r
			WHERE r.name_search @@ plainto_tsquery('english', $1)
		)
		UNION ALL
		(
			SELECT 
				'dish' as type,
				mi.id,
				mi.name,
				mi.restaurant_id,
				r.name as restaurant_name,
				mi.price,
				ts_rank(mi.name_search, plainto_tsquery('english', $1)) as relevance
			FROM menu_items mi
			INNER JOIN restaurants r ON r.id = mi.restaurant_id
			WHERE mi.is_active = TRUE
			AND mi.name_search @@ plainto_tsquery('english', $1)
		)
		ORDER BY relevance DESC, name
		LIMIT $2
	`

	err := r.db.Select(&results, sqlQuery, queryTerm, limit)
	if err != nil {
		return nil, err
	}

	return results, nil
}
