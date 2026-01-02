package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

// QueryRecorder is an interface for recording database query metrics
// This allows the db package to be decoupled from the metrics implementation
type QueryRecorder interface {
	RecordDBQuery(query string, duration time.Duration, hasError bool)
}

// DBWrapper wraps sqlx.DB to track query execution times
type DBWrapper struct {
	db       *sqlx.DB
	recorder QueryRecorder
}

// NewDBWrapper creates a new database wrapper with metrics tracking
func NewDBWrapper(db *sqlx.DB, recorder QueryRecorder) *DBWrapper {
	return &DBWrapper{
		db:       db,
		recorder: recorder,
	}
}

// GetDB returns the underlying sqlx.DB (for methods not wrapped yet)
func (w *DBWrapper) GetDB() *sqlx.DB {
	return w.db
}

// Select executes a query and scans the results into dest, tracking execution time
func (w *DBWrapper) Select(dest interface{}, query string, args ...interface{}) error {
	start := time.Now()
	err := w.db.Select(dest, query, args...)
	duration := time.Since(start)
	hasError := err != nil

	// Normalize query for metrics (remove extra whitespace)
	normalizedQuery := normalizeQuery(query)
	if w.recorder != nil {
		w.recorder.RecordDBQuery(normalizedQuery, duration, hasError)
	}

	return err
}

// Get executes a query and scans one row into dest, tracking execution time
func (w *DBWrapper) Get(dest interface{}, query string, args ...interface{}) error {
	start := time.Now()
	err := w.db.Get(dest, query, args...)
	duration := time.Since(start)
	hasError := err != nil

	// Normalize query for metrics (remove extra whitespace)
	normalizedQuery := normalizeQuery(query)
	if w.recorder != nil {
		w.recorder.RecordDBQuery(normalizedQuery, duration, hasError)
	}

	return err
}

// Exec executes a query without returning rows, tracking execution time
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
func (w *DBWrapper) Beginx() (*TxWrapper, error) {
	tx, err := w.db.Beginx()
	if err != nil {
		return nil, err
	}
	return NewTxWrapper(tx, w.recorder), nil
}

// BeginTxx starts a transaction with context, returning a wrapped transaction
func (w *DBWrapper) BeginTxx(ctx context.Context, opts *sql.TxOptions) (*TxWrapper, error) {
	tx, err := w.db.BeginTxx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return NewTxWrapper(tx, w.recorder), nil
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
type TxWrapper struct {
	tx       *sqlx.Tx
	recorder QueryRecorder
}

// NewTxWrapper creates a new transaction wrapper with metrics tracking
func NewTxWrapper(tx *sqlx.Tx, recorder QueryRecorder) *TxWrapper {
	return &TxWrapper{
		tx:       tx,
		recorder: recorder,
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
func (w *TxWrapper) Exec(query string, args ...interface{}) (sql.Result, error) {
	start := time.Now()
	result, err := w.tx.Exec(query, args...)
	duration := time.Since(start)
	hasError := err != nil

	normalizedQuery := normalizeQuery(query)
	if w.recorder != nil {
		w.recorder.RecordDBQuery(normalizedQuery, duration, hasError)
	}

	return result, err
}

// Commit commits the transaction
func (w *TxWrapper) Commit() error {
	return w.tx.Commit()
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
