package firewall

import (
	"encoding/json"
	"net/http"
)

// WriteJSONError sends a JSON error response with the specified status code
func WriteJSONError(w http.ResponseWriter, status int, success bool, err string, details string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	response := map[string]interface{}{
		"success": success,
		"error":   err,
		"details": details,
	}

	_ = json.NewEncoder(w).Encode(response)
}
