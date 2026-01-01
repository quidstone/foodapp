package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/jmoiron/sqlx"
)

// QueryRecorder is an interface for recording database query metrics
// This allows the db package to be decoupled from the metrics implementation
type QueryRecorder interface {
	RecordDBQuery(query string, duration time.Duration, hasError bool)
}

// DBWrapper wraps sqlx.DB to track query execution times and optionally cache results
type DBWrapper struct {
	db       *sqlx.DB
	recorder QueryRecorder
	cache    Cache
	// Default TTL for cached queries (0 = use cache's default)
	defaultCacheTTL time.Duration
}

// NewDBWrapper creates a new database wrapper with metrics tracking
func NewDBWrapper(db *sqlx.DB, recorder QueryRecorder) *DBWrapper {
	return &DBWrapper{
		db:       db,
		recorder: recorder,
	}
}

// NewDBWrapperWithCache creates a new database wrapper with metrics tracking and caching
func NewDBWrapperWithCache(db *sqlx.DB, recorder QueryRecorder, cache Cache, defaultCacheTTL time.Duration) *DBWrapper {
	return &DBWrapper{
		db:              db,
		recorder:        recorder,
		cache:           cache,
		defaultCacheTTL: defaultCacheTTL,
	}
}

// SetCache sets the cache for the wrapper
func (w *DBWrapper) SetCache(cache Cache, defaultTTL time.Duration) {
	w.cache = cache
	w.defaultCacheTTL = defaultTTL
}

// GetDB returns the underlying sqlx.DB (for methods not wrapped yet)
func (w *DBWrapper) GetDB() *sqlx.DB {
	return w.db
}

// Select executes a query and scans the results into dest, tracking execution time
// If caching is enabled and the query is read-only, it will check the cache first
func (w *DBWrapper) Select(dest interface{}, query string, args ...interface{}) error {
	// Check cache if enabled and query is read-only
	if w.cache != nil && isReadOnlyQuery(query) {
		cacheKey := generateCacheKey(query, args...)
		if cached, found := w.cache.Get(cacheKey); found {
			// Cache hit - unmarshal and return
			if err := json.Unmarshal(cached, dest); err == nil {
				// Record cache hit with minimal duration
				if w.recorder != nil {
					w.recorder.RecordDBQuery(normalizeQuery(query), 0, false)
				}
				return nil
			}
			// If unmarshal fails, fall through to database query
		}
	}

	// Cache miss or cache disabled - execute query
	start := time.Now()
	err := w.db.Select(dest, query, args...)
	duration := time.Since(start)
	hasError := err != nil

	// Normalize query for metrics (remove extra whitespace)
	normalizedQuery := normalizeQuery(query)
	if w.recorder != nil {
		w.recorder.RecordDBQuery(normalizedQuery, duration, hasError)
	}

	// Cache the result if successful and caching is enabled
	if err == nil && w.cache != nil && isReadOnlyQuery(query) {
		if data, marshalErr := json.Marshal(dest); marshalErr == nil {
			cacheKey := generateCacheKey(query, args...)
			// Tag with table names for targeted invalidation
			tables := extractTablesFromSelect(query)
			w.cache.SetWithTags(cacheKey, data, w.defaultCacheTTL, tables)
		}
	}

	return err
}

// Get executes a query and scans one row into dest, tracking execution time
// If caching is enabled and the query is read-only, it will check the cache first
func (w *DBWrapper) Get(dest interface{}, query string, args ...interface{}) error {
	// Check cache if enabled and query is read-only
	if w.cache != nil && isReadOnlyQuery(query) {
		cacheKey := generateCacheKey(query, args...)
		if cached, found := w.cache.Get(cacheKey); found {
			// Cache hit - unmarshal and return
			if err := json.Unmarshal(cached, dest); err == nil {
				// Record cache hit with minimal duration
				if w.recorder != nil {
					w.recorder.RecordDBQuery(normalizeQuery(query), 0, false)
				}
				return nil
			}
			// If unmarshal fails, fall through to database query
		}
	}

	// Cache miss or cache disabled - execute query
	start := time.Now()
	err := w.db.Get(dest, query, args...)
	duration := time.Since(start)
	hasError := err != nil

	// Normalize query for metrics (remove extra whitespace)
	normalizedQuery := normalizeQuery(query)
	if w.recorder != nil {
		w.recorder.RecordDBQuery(normalizedQuery, duration, hasError)
	}

	// Cache the result if successful and caching is enabled
	if err == nil && w.cache != nil && isReadOnlyQuery(query) {
		if data, marshalErr := json.Marshal(dest); marshalErr == nil {
			cacheKey := generateCacheKey(query, args...)
			// Tag with table names for targeted invalidation
			tables := extractTablesFromSelect(query)
			w.cache.SetWithTags(cacheKey, data, w.defaultCacheTTL, tables)
		}
	}

	return err
}

// Exec executes a query without returning rows, tracking execution time
// Write operations (INSERT, UPDATE, DELETE) will invalidate affected cache entries
func (w *DBWrapper) Exec(query string, args ...interface{}) (sql.Result, error) {
	start := time.Now()
	result, err := w.db.Exec(query, args...)
	duration := time.Since(start)
	hasError := err != nil

	// Normalize query for metrics (remove extra whitespace)
	normalizedQuery := normalizeQuery(query)
	if w.recorder != nil {
		w.recorder.RecordDBQuery(normalizedQuery, duration, hasError)
	}

	// Invalidate cache entries for the affected table on write operations
	if w.cache != nil && !isReadOnlyQuery(query) {
		table := extractTableFromWrite(query)
		if table != "" {
			// Only invalidate cache entries tagged with this table
			w.cache.DeleteByTag(table)
		}
	}

	return result, err
}

// Query executes a query and returns rows, tracking execution time
func (w *DBWrapper) Query(query string, args ...interface{}) (*sql.Rows, error) {
	start := time.Now()
	rows, err := w.db.Query(query, args...)
	duration := time.Since(start)
	hasError := err != nil

	// Normalize query for metrics (remove extra whitespace)
	normalizedQuery := normalizeQuery(query)
	if w.recorder != nil {
		w.recorder.RecordDBQuery(normalizedQuery, duration, hasError)
	}

	return rows, err
}

// Queryx executes a query and returns sqlx.Rows, tracking execution time
func (w *DBWrapper) Queryx(query string, args ...interface{}) (*sqlx.Rows, error) {
	start := time.Now()
	rows, err := w.db.Queryx(query, args...)
	duration := time.Since(start)
	hasError := err != nil

	// Normalize query for metrics (remove extra whitespace)
	normalizedQuery := normalizeQuery(query)
	if w.recorder != nil {
		w.recorder.RecordDBQuery(normalizedQuery, duration, hasError)
	}

	return rows, err
}

// QueryRowx executes a query and returns a single row, tracking execution time
func (w *DBWrapper) QueryRowx(query string, args ...interface{}) *sqlx.Row {
	start := time.Now()
	row := w.db.QueryRowx(query, args...)
	duration := time.Since(start)

	// Note: QueryRowx doesn't return an error immediately, so we can't track errors here
	// The error will be returned when scanning. For now, we'll track it as successful.
	normalizedQuery := normalizeQuery(query)
	if w.recorder != nil {
		w.recorder.RecordDBQuery(normalizedQuery, duration, false)
	}

	return row
}

// Beginx starts a transaction, returning a wrapped transaction
// Transactions don't use cache to ensure consistency
func (w *DBWrapper) Beginx() (*TxWrapper, error) {
	tx, err := w.db.Beginx()
	if err != nil {
		return nil, err
	}
	return NewTxWrapper(tx, w.recorder, w.cache), nil
}

// BeginTxx starts a transaction with context, returning a wrapped transaction
// Transactions don't use cache to ensure consistency
func (w *DBWrapper) BeginTxx(ctx context.Context, opts *sql.TxOptions) (*TxWrapper, error) {
	tx, err := w.db.BeginTxx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return NewTxWrapper(tx, w.recorder, w.cache), nil
}

// PingContext pings the database with context
func (w *DBWrapper) PingContext(ctx context.Context) error {
	return w.db.PingContext(ctx)
}

// SetConnMaxLifetime sets the maximum connection lifetime
func (w *DBWrapper) SetConnMaxLifetime(d time.Duration) {
	w.db.SetConnMaxLifetime(d)
}

// Close closes the database connection
func (w *DBWrapper) Close() error {
	return w.db.Close()
}

// TxWrapper wraps sqlx.Tx to track query execution times in transactions
// Transactions don't use cache to ensure consistency
type TxWrapper struct {
	tx             *sqlx.Tx
	recorder       QueryRecorder
	cache          Cache
	modifiedTables map[string]struct{} // tracks tables modified in this transaction
}

// NewTxWrapper creates a new transaction wrapper with metrics tracking
func NewTxWrapper(tx *sqlx.Tx, recorder QueryRecorder, cache Cache) *TxWrapper {
	return &TxWrapper{
		tx:             tx,
		recorder:       recorder,
		cache:          cache,
		modifiedTables: make(map[string]struct{}),
	}
}

// GetTx returns the underlying sqlx.Tx
func (w *TxWrapper) GetTx() *sqlx.Tx {
	return w.tx
}

// Get executes a query and scans one row into dest, tracking execution time
func (w *TxWrapper) Get(dest interface{}, query string, args ...interface{}) error {
	start := time.Now()
	err := w.tx.Get(dest, query, args...)
	duration := time.Since(start)
	hasError := err != nil

	normalizedQuery := normalizeQuery(query)
	if w.recorder != nil {
		w.recorder.RecordDBQuery(normalizedQuery, duration, hasError)
	}

	return err
}

// Select executes a query and scans the results into dest, tracking execution time
func (w *TxWrapper) Select(dest interface{}, query string, args ...interface{}) error {
	start := time.Now()
	err := w.tx.Select(dest, query, args...)
	duration := time.Since(start)
	hasError := err != nil

	normalizedQuery := normalizeQuery(query)
	if w.recorder != nil {
		w.recorder.RecordDBQuery(normalizedQuery, duration, hasError)
	}

	return err
}

// Exec executes a query without returning rows, tracking execution time
// Write operations in transactions will invalidate cache on commit
func (w *TxWrapper) Exec(query string, args ...interface{}) (sql.Result, error) {
	start := time.Now()
	result, err := w.tx.Exec(query, args...)
	duration := time.Since(start)
	hasError := err != nil

	normalizedQuery := normalizeQuery(query)
	if w.recorder != nil {
		w.recorder.RecordDBQuery(normalizedQuery, duration, hasError)
	}

	// Track modified table for cache invalidation on commit
	if !isReadOnlyQuery(query) {
		table := extractTableFromWrite(query)
		if table != "" {
			w.modifiedTables[table] = struct{}{}
		}
	}

	return result, err
}

// Commit commits the transaction and invalidates cache for modified tables
func (w *TxWrapper) Commit() error {
	err := w.tx.Commit()
	// Invalidate cache entries for modified tables on successful commit
	if err == nil && w.cache != nil && len(w.modifiedTables) > 0 {
		for table := range w.modifiedTables {
			w.cache.DeleteByTag(table)
		}
	}
	return err
}

// Rollback rolls back the transaction
func (w *TxWrapper) Rollback() error {
	return w.tx.Rollback()
}

// normalizeQuery normalizes a SQL query for metrics tracking
// Removes extra whitespace and limits length for better grouping
func normalizeQuery(query string) string {
	// For now, just return a simplified version
	// In production, you might want to extract just the operation type (SELECT, INSERT, etc.)
	// or use a query fingerprinting library
	if len(query) > 100 {
		// Truncate very long queries and add operation type if possible
		return query[:100] + "..."
	}
	return query
}
