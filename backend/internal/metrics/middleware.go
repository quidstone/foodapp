package metrics

import (
	"net/http"
	"time"
)

// ResponseWriter wrapper to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.statusCode = http.StatusOK
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

// Middleware returns an HTTP middleware that tracks API response times
func Middleware(collector *MetricsCollector) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap the response writer to capture status code
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Call the next handler
			next.ServeHTTP(wrapped, r)

			// Record metrics
			duration := time.Since(start)
			hasError := wrapped.statusCode >= 400
			// Use URL path instead of RequestURI to avoid query parameters
			endpoint := r.URL.Path
			if endpoint == "" {
				endpoint = r.RequestURI
			}
			collector.RecordAPICall(endpoint, duration, hasError)
		})
	}
}

// WrapHandler wraps an http.HandlerFunc with metrics tracking
func WrapHandler(collector *MetricsCollector, endpoint string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap the response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Call the handler
		handler(wrapped, r)

		// Record metrics
		duration := time.Since(start)
		hasError := wrapped.statusCode >= 400
		collector.RecordAPICall(endpoint, duration, hasError)
	}
}
