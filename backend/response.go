package main

import (
	"encoding/json"
	"log"
	"net/http"
)

type Response struct {
	Data  interface{} `json:"data,omitempty"`
	Error string      `json:"error,omitempty"`
}

// writeError writes a JSON error response with the given status code and message
func writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	apiErr := Response{Error: message}
	if err := json.NewEncoder(w).Encode(apiErr); err != nil {
		log.Printf("Error encoding error response: %v", err)
	}
}

// writeResponse writes a JSON response with the given data and status code
func writeResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	response := Response{Data: data}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
	}
}
