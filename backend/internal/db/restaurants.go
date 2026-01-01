package db

import (
	"time"
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
	db *DBWrapper
}

// NewRestaurantRepository creates a new restaurant repository
func NewRestaurantRepository(db *DBWrapper) *RestaurantRepository {
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

	// Use separate queries to avoid string concatenation of operators
	// This eliminates any SQL injection risk from the comparison parameter
	var query string
	switch comparison {
	case "less", "fewer", "lt", "<":
		query = `
			SELECT r.id, r.name, r.cash_balance, r.timezone, COUNT(mi.id) as dish_count
			FROM restaurants r
			INNER JOIN menu_items mi ON mi.restaurant_id = r.id
			WHERE mi.is_active = TRUE
			AND mi.price >= $1 AND mi.price <= $2
			GROUP BY r.id, r.name, r.cash_balance, r.timezone
			HAVING COUNT(mi.id) < $3
			ORDER BY r.name
			LIMIT $4
		`
	default:
		// "more", "greater", "gt", ">", or any other value defaults to "more"
		query = `
			SELECT r.id, r.name, r.cash_balance, r.timezone, COUNT(mi.id) as dish_count
			FROM restaurants r
			INNER JOIN menu_items mi ON mi.restaurant_id = r.id
			WHERE mi.is_active = TRUE
			AND mi.price >= $1 AND mi.price <= $2
			GROUP BY r.id, r.name, r.cash_balance, r.timezone
			HAVING COUNT(mi.id) > $3
			ORDER BY r.name
			LIMIT $4
		`
	}

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
// with prefix matching and trigram similarity for typo tolerance.
// Returns results ranked by relevance.
func (r *RestaurantRepository) Search(queryTerm string, limit int) ([]SearchResult, error) {
	var results []SearchResult

	// Combined search using:
	// 1. Full-text search with prefix matching (e.g., "piz" matches "pizza")
	// 2. Word similarity for typo tolerance (e.g., "piza" matches "Pizza Margherita")
	//    Uses word_similarity() which finds the best matching substring in the target,
	//    so "piza" matches "Pepperoni Pizza Deluxe" by comparing against "Pizza".
	// 3. ILIKE fallback for simple substring matching
	//
	// The query transforms input like "spicy chi" into "spicy:* & chi:*" for prefix matching.
	// Results are ranked by the best score from either ts_rank or word similarity.
	sqlQuery := `
		WITH search_query AS (
			SELECT
				regexp_replace(trim($1), '\s+', ':* & ', 'g') || ':*' AS prefix_query,
				$1 AS raw_query
		)
		(
			SELECT
				'restaurant' as type,
				r.id,
				r.name,
				NULL::BIGINT as restaurant_id,
				NULL::TEXT as restaurant_name,
				NULL::NUMERIC as price,
				GREATEST(
					COALESCE(ts_rank(r.name_search, to_tsquery('english', sq.prefix_query)), 0),
					word_similarity(sq.raw_query, r.name)
				) as relevance
			FROM restaurants r, search_query sq
			WHERE r.name_search @@ to_tsquery('english', sq.prefix_query)
			   OR word_similarity(sq.raw_query, r.name) > 0.3
			   OR r.name ILIKE '%' || sq.raw_query || '%'
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
				GREATEST(
					COALESCE(ts_rank(mi.name_search, to_tsquery('english', sq.prefix_query)), 0),
					word_similarity(sq.raw_query, mi.name)
				) as relevance
			FROM menu_items mi
			INNER JOIN restaurants r ON r.id = mi.restaurant_id
			CROSS JOIN search_query sq
			WHERE mi.is_active = TRUE
			AND (
				mi.name_search @@ to_tsquery('english', sq.prefix_query)
				OR word_similarity(sq.raw_query, mi.name) > 0.3
				OR mi.name ILIKE '%' || sq.raw_query || '%'
			)
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
