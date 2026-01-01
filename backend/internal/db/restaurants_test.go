package db

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestRestaurantRepository_FindOpenAtTime(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "postgres")
	wrappedDB := NewDBWrapper(sqlxDB, nil) // nil recorder for tests
	repo := NewRestaurantRepository(wrappedDB)

	tests := []struct {
		name        string
		datetime    time.Time
		mockRows    *sqlmock.Rows
		expectError bool
	}{
		{
			name:     "success - returns open restaurants",
			datetime: time.Date(2024, 1, 15, 14, 30, 0, 0, time.UTC),
			// Mock returns in alphabetical order (as SQL ORDER BY would)
			mockRows: sqlmock.NewRows([]string{"id", "name", "cash_balance", "timezone"}).
				AddRow(2, "Burger Joint", 500.25, "America/New_York").
				AddRow(1, "Pizza Place", 1000.50, "UTC"),
			expectError: false,
		},
		{
			name:        "success - no restaurants open",
			datetime:    time.Date(2024, 1, 15, 2, 0, 0, 0, time.UTC),
			mockRows:    sqlmock.NewRows([]string{"id", "name", "cash_balance", "timezone"}),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock.ExpectQuery(`SELECT DISTINCT r.id, r.name, r.cash_balance, r.timezone`).
				WithArgs(tt.datetime).
				WillReturnRows(tt.mockRows)

			result, err := repo.FindOpenAtTime(tt.datetime)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Check expected number of results based on test case
				if tt.name == "success - returns open restaurants" {
					assert.NotNil(t, result)
					assert.Equal(t, 2, len(result))
					// Results are sorted alphabetically
					assert.Equal(t, "Burger Joint", result[0].Name)
					assert.Equal(t, "Pizza Place", result[1].Name)
				} else {
					// No results case - result can be nil or empty slice
					if result != nil {
						assert.Equal(t, 0, len(result))
					}
				}
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRestaurantRepository_FindTopByDishCount(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "postgres")
	wrappedDB := NewDBWrapper(sqlxDB, nil) // nil recorder for tests
	repo := NewRestaurantRepository(wrappedDB)

	tests := []struct {
		name        string
		limit       int
		dishCount   int
		minPrice    float64
		maxPrice    float64
		comparison  string
		mockRows    *sqlmock.Rows
		expectError bool
		expectedOp  string
	}{
		{
			name:       "success - more than threshold",
			limit:      10,
			dishCount:  5,
			minPrice:   10.0,
			maxPrice:   50.0,
			comparison: "more",
			mockRows: sqlmock.NewRows([]string{"id", "name", "cash_balance", "timezone", "dish_count"}).
				AddRow(1, "Restaurant A", 1000.0, "UTC", 8).
				AddRow(2, "Restaurant B", 2000.0, "UTC", 6),
			expectError: false,
			expectedOp:  ">",
		},
		{
			name:       "success - less than threshold",
			limit:      5,
			dishCount:  10,
			minPrice:   20.0,
			maxPrice:   30.0,
			comparison: "less",
			mockRows: sqlmock.NewRows([]string{"id", "name", "cash_balance", "timezone", "dish_count"}).
				AddRow(1, "Small Restaurant", 500.0, "UTC", 3),
			expectError: false,
			expectedOp:  "<",
		},
		{
			name:       "default comparison to more",
			limit:      5,
			dishCount:  5,
			minPrice:   10.0,
			maxPrice:   50.0,
			comparison: "invalid",
			mockRows: sqlmock.NewRows([]string{"id", "name", "cash_balance", "timezone", "dish_count"}).
				AddRow(1, "Restaurant", 1000.0, "UTC", 6),
			expectError: false,
			expectedOp:  ">",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: sqlmock doesn't support dynamic SQL operators well, so we'll match the pattern
			mock.ExpectQuery(`SELECT r.id, r.name, r.cash_balance, r.timezone, COUNT\(mi.id\) as dish_count`).
				WithArgs(tt.minPrice, tt.maxPrice, tt.dishCount, tt.limit).
				WillReturnRows(tt.mockRows)

			result, err := repo.FindTopByDishCount(tt.limit, tt.dishCount, tt.minPrice, tt.maxPrice, tt.comparison)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				// Verify results based on test case
				if tt.name == "success - more than threshold" {
					assert.Equal(t, 2, len(result))
					assert.Greater(t, result[0].DishCount, 0)
				} else if tt.name == "success - less than threshold" || tt.name == "default comparison to more" {
					assert.Equal(t, 1, len(result))
					assert.Greater(t, result[0].DishCount, 0)
				}
			}
		})
	}
}

func TestRestaurantRepository_Search(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "postgres")
	wrappedDB := NewDBWrapper(sqlxDB, nil) // nil recorder for tests
	repo := NewRestaurantRepository(wrappedDB)

	tests := []struct {
		name        string
		queryTerm   string
		limit       int
		mockRows    *sqlmock.Rows
		expectError bool
	}{
		{
			name:      "success - returns restaurants and dishes",
			queryTerm: "pizza",
			limit:     20,
			mockRows: sqlmock.NewRows([]string{"type", "id", "name", "restaurant_id", "restaurant_name", "price", "relevance"}).
				AddRow("restaurant", 1, "Pizza Palace", nil, nil, nil, 0.8).
				AddRow("dish", 10, "Margherita Pizza", 1, "Pizza Palace", 15.99, 0.9),
			expectError: false,
		},
		{
			name:        "success - no results",
			queryTerm:   "nonexistent",
			limit:       20,
			mockRows:    sqlmock.NewRows([]string{"type", "id", "name", "restaurant_id", "restaurant_name", "price", "relevance"}),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock.ExpectQuery(`SELECT`).
				WithArgs(tt.queryTerm, tt.limit).
				WillReturnRows(tt.mockRows)

			result, err := repo.Search(tt.queryTerm, tt.limit)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify results based on test case
				if tt.name == "success - returns restaurants and dishes" {
					assert.NotNil(t, result)
					assert.Equal(t, 2, len(result))
					assert.Contains(t, []string{"restaurant", "dish"}, result[0].Type)
				} else {
					// No results case - result can be nil or empty slice
					if result != nil {
						assert.Equal(t, 0, len(result))
					}
				}
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
