package firewall

import (
	"encoding/json"
	"net/http"
)

// getOption retrieves an option value, checking the new dot-separated key first,
// then falling back to the legacy underscore key for backward compatibility
func getOption(options map[string]string, newKey, legacyKey string) string {
	if val, ok := options[newKey]; ok {
		return val
	}
	return options[legacyKey]
}

// WriteJSONError sends a JSON error response with the specified status code
func WriteJSONError(w http.ResponseWriter, status int, success bool, err string, details string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	response := map[string]any{
		"success": success,
		"error":   err,
		"details": details,
	}

	_ = json.NewEncoder(w).Encode(response)
}
