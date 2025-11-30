package wsrelay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetOption(t *testing.T) {
	t.Run("returns new key value when present", func(t *testing.T) {
		options := map[string]string{
			"new.key":    "new_value",
			"legacy_key": "legacy_value",
		}

		result := getOption(options, "new.key", "legacy_key")
		assert.Equal(t, "new_value", result)
	})

	t.Run("falls back to legacy key when new key missing", func(t *testing.T) {
		options := map[string]string{
			"legacy_key": "legacy_value",
		}

		result := getOption(options, "new.key", "legacy_key")
		assert.Equal(t, "legacy_value", result)
	})

	t.Run("returns empty string when both keys missing", func(t *testing.T) {
		options := map[string]string{}

		result := getOption(options, "new.key", "legacy_key")
		assert.Equal(t, "", result)
	})

	t.Run("prefers new key even when empty string", func(t *testing.T) {
		options := map[string]string{
			"new.key":    "",
			"legacy_key": "legacy_value",
		}

		result := getOption(options, "new.key", "legacy_key")
		assert.Equal(t, "", result)
	})
}

func TestResponseWrapper(t *testing.T) {
	t.Run("wraps ResponseWriter correctly", func(t *testing.T) {
		w := httptest.NewRecorder()
		rw := newResponseWrapper(w)

		rw.Header().Set("X-Test", "value")
		assert.Equal(t, "value", rw.Header().Get("X-Test"))
	})

	t.Run("Write passes through", func(t *testing.T) {
		w := httptest.NewRecorder()
		rw := newResponseWrapper(w)

		n, err := rw.Write([]byte("hello"))
		assert.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, "hello", w.Body.String())
	})

	t.Run("WriteHeader passes through", func(t *testing.T) {
		w := httptest.NewRecorder()
		rw := newResponseWrapper(w)

		rw.WriteHeader(http.StatusCreated)
		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("Flush works with flusher", func(t *testing.T) {
		w := httptest.NewRecorder()
		rw := newResponseWrapper(w)

		// Should not panic
		rw.Flush()
		assert.True(t, w.Flushed)
	})
}

func TestOriginPatternParsing(t *testing.T) {
	// Test the origin pattern parsing logic extracted from CreateMiddleware
	parseOriginPatterns := func(allowedOrigins string) []string {
		if allowedOrigins == "" {
			return []string{"*"}
		}

		var patterns []string
		for _, origin := range splitAndTrim(allowedOrigins, ",") {
			if origin != "" {
				patterns = append(patterns, origin)
			}
		}

		if len(patterns) == 0 {
			return []string{"*"}
		}
		return patterns
	}

	t.Run("empty string defaults to wildcard", func(t *testing.T) {
		patterns := parseOriginPatterns("")
		assert.Equal(t, []string{"*"}, patterns)
	})

	t.Run("single origin", func(t *testing.T) {
		patterns := parseOriginPatterns("https://example.com")
		assert.Equal(t, []string{"https://example.com"}, patterns)
	})

	t.Run("multiple origins comma separated", func(t *testing.T) {
		patterns := parseOriginPatterns("https://example.com,https://test.com")
		assert.Equal(t, []string{"https://example.com", "https://test.com"}, patterns)
	})

	t.Run("trims whitespace", func(t *testing.T) {
		patterns := parseOriginPatterns("  https://example.com  ,  https://test.com  ")
		assert.Equal(t, []string{"https://example.com", "https://test.com"}, patterns)
	})

	t.Run("wildcard pattern", func(t *testing.T) {
		patterns := parseOriginPatterns("*")
		assert.Equal(t, []string{"*"}, patterns)
	})
}

// splitAndTrim splits a string by sep and trims each part
func splitAndTrim(s, sep string) []string {
	var result []string
	if s == "" {
		return result
	}

	start := 0
	for i := 0; i < len(s); i++ {
		if i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			part := trim(s[start:i])
			if part != "" {
				result = append(result, part)
			}
			start = i + len(sep)
		}
	}

	part := trim(s[start:])
	if part != "" {
		result = append(result, part)
	}
	return result
}

func trim(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}

func TestRelayCommandConstants(t *testing.T) {
	t.Run("option keys defined", func(t *testing.T) {
		assert.Equal(t, "wsrelay.allowed.origins", OptionAllowedOrigins)
		assert.Equal(t, "allowed_origins", legacyAllowedOrigins)
	})

	t.Run("topic constants defined", func(t *testing.T) {
		assert.Equal(t, "ws.message", WSMessageTopic)
		assert.Equal(t, "ws.join", WSJoinTopic)
		assert.Equal(t, "ws.leave", WSLeaveTopic)
		assert.Equal(t, "ws.control", WSControlTopic)
		assert.Equal(t, "ws.close", WSCloseTopic)
		assert.Equal(t, "ws.heartbeat", WSHeartbeatTopic)
	})

	t.Run("header constant defined", func(t *testing.T) {
		assert.Equal(t, "X-WS-Relay", WSRelayHeader)
	})
}
