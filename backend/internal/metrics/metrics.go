package metrics

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// DurationString is a wrapper around time.Duration that marshals to a human-readable string
type DurationString struct {
	time.Duration
}

// MarshalJSON implements json.Marshaler for DurationString
func (d DurationString) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

// APIMetrics stores metrics for an API endpoint
type APIMetrics struct {
	Endpoint         string         `json:"endpoint"`
	RequestCount     int64          `json:"request_count"`
	TotalResponseTime DurationString `json:"total_response_time"`
	MinResponseTime  DurationString `json:"min_response_time"`
	MaxResponseTime  DurationString `json:"max_response_time"`
	ErrorCount       int64          `json:"error_count"`
	LastUpdated      time.Time      `json:"last_updated"`
}

// DBQueryMetrics stores metrics for a database query
type DBQueryMetrics struct {
	Query          string         `json:"query"`
	ExecutionCount int64          `json:"execution_count"`
	TotalDuration  DurationString `json:"total_duration"`
	MinDuration    DurationString `json:"min_duration"`
	MaxDuration    DurationString `json:"max_duration"`
	ErrorCount     int64          `json:"error_count"`
	LastUpdated    time.Time      `json:"last_updated"`
}

// MetricsCollector collects metrics for API endpoints and DB queries
type MetricsCollector struct {
	mu          sync.RWMutex
	apiMetrics  map[string]*APIMetrics
	dbMetrics   map[string]*DBQueryMetrics
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		apiMetrics: make(map[string]*APIMetrics),
		dbMetrics:  make(map[string]*DBQueryMetrics),
	}
}

// RecordAPICall records an API endpoint call with its response time
func (mc *MetricsCollector) RecordAPICall(endpoint string, duration time.Duration, hasError bool) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if _, exists := mc.apiMetrics[endpoint]; !exists {
		mc.apiMetrics[endpoint] = &APIMetrics{
			Endpoint:        endpoint,
			MinResponseTime: DurationString{duration},
			MaxResponseTime: DurationString{duration},
		}
	}

	metrics := mc.apiMetrics[endpoint]
	metrics.RequestCount++
	metrics.TotalResponseTime.Duration += duration
	metrics.LastUpdated = time.Now()

	// Update min/max
	if duration < metrics.MinResponseTime.Duration {
		metrics.MinResponseTime = DurationString{duration}
	}
	if duration > metrics.MaxResponseTime.Duration {
		metrics.MaxResponseTime = DurationString{duration}
	}

	if hasError {
		metrics.ErrorCount++
	}
}

// RecordDBQuery records a database query execution time
func (mc *MetricsCollector) RecordDBQuery(query string, duration time.Duration, hasError bool) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if _, exists := mc.dbMetrics[query]; !exists {
		mc.dbMetrics[query] = &DBQueryMetrics{
			Query:       query,
			MinDuration: DurationString{duration},
			MaxDuration: DurationString{duration},
		}
	}

	metrics := mc.dbMetrics[query]
	metrics.ExecutionCount++
	metrics.TotalDuration.Duration += duration
	metrics.LastUpdated = time.Now()

	// Update min/max
	if duration < metrics.MinDuration.Duration {
		metrics.MinDuration = DurationString{duration}
	}
	if duration > metrics.MaxDuration.Duration {
		metrics.MaxDuration = DurationString{duration}
	}

	if hasError {
		metrics.ErrorCount++
	}
}

// GetAPIMetrics returns all API metrics
func (mc *MetricsCollector) GetAPIMetrics() map[string]*APIMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	// Return a copy
	result := make(map[string]*APIMetrics)
	for k, v := range mc.apiMetrics {
		result[k] = v
	}
	return result
}

// GetDBMetrics returns all DB query metrics
func (mc *MetricsCollector) GetDBMetrics() map[string]*DBQueryMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	// Return a copy
	result := make(map[string]*DBQueryMetrics)
	for k, v := range mc.dbMetrics {
		result[k] = v
	}
	return result
}

// GetAPIStats returns average, min, max for an endpoint
func (mc *MetricsCollector) GetAPIStats(endpoint string) (avg, min, max time.Duration, count int64, errors int64) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if metrics, exists := mc.apiMetrics[endpoint]; exists {
		if metrics.RequestCount > 0 {
			avg = time.Duration(int64(metrics.TotalResponseTime.Duration) / metrics.RequestCount)
		}
		return avg, metrics.MinResponseTime.Duration, metrics.MaxResponseTime.Duration, metrics.RequestCount, metrics.ErrorCount
	}
	return 0, 0, 0, 0, 0
}

// GetDBStats returns average, min, max for a query
func (mc *MetricsCollector) GetDBStats(query string) (avg, min, max time.Duration, count int64, errors int64) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if metrics, exists := mc.dbMetrics[query]; exists {
		if metrics.ExecutionCount > 0 {
			avg = time.Duration(int64(metrics.TotalDuration.Duration) / metrics.ExecutionCount)
		}
		return avg, metrics.MinDuration.Duration, metrics.MaxDuration.Duration, metrics.ExecutionCount, metrics.ErrorCount
	}
	return 0, 0, 0, 0, 0
}

// PrintSummary prints a summary of all metrics
func (mc *MetricsCollector) PrintSummary() string {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	summary := "=== API METRICS ===\n"
	for endpoint, metrics := range mc.apiMetrics {
		if metrics.RequestCount > 0 {
			avg := time.Duration(int64(metrics.TotalResponseTime.Duration) / metrics.RequestCount)
			summary += fmt.Sprintf(
				"Endpoint: %s | Requests: %d | Avg: %v | Min: %v | Max: %v | Errors: %d\n",
				endpoint, metrics.RequestCount, avg, metrics.MinResponseTime.Duration, metrics.MaxResponseTime.Duration, metrics.ErrorCount,
			)
		}
	}

	summary += "\n=== DB QUERY METRICS ===\n"
	for query, metrics := range mc.dbMetrics {
		if metrics.ExecutionCount > 0 {
			avg := time.Duration(int64(metrics.TotalDuration.Duration) / metrics.ExecutionCount)
			summary += fmt.Sprintf(
				"Query: %s | Executions: %d | Avg: %v | Min: %v | Max: %v | Errors: %d\n",
				query, metrics.ExecutionCount, avg, metrics.MinDuration.Duration, metrics.MaxDuration.Duration, metrics.ErrorCount,
			)
		}
	}

	return summary
}
