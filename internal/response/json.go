package response

import (
	"encoding/json"
	"net/http"
)

// JSON sends a standard JSON response with the given status code
func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

// Error sends a standard JSON error response
func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, map[string]string{
		"error": message,
	})
}

// Success sends a standard JSON success response with an optional message
func Success(w http.ResponseWriter, message string, data interface{}) {
	payload := map[string]interface{}{}
	if message != "" {
		payload["message"] = message
	}
	if data != nil {
		payload["data"] = data
	}
	JSON(w, http.StatusOK, payload)
}
