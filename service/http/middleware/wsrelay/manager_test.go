package wsrelay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetOrigins(t *testing.T) {
	t.Run("returns module-specific key first", func(t *testing.T) {
		options := map[string]string{
			"wsrelay.allowed.origins": "example.com",
			"allow_origins":           "shared.com",
			"allowed_origins":         "legacy.com",
		}

		result := getOrigins(options)
		assert.Equal(t, "example.com", result)
	})

	t.Run("falls back to shared key", func(t *testing.T) {
		options := map[string]string{
			"allow_origins":   "shared.com",
			"allowed_origins": "legacy.com",
		}

		result := getOrigins(options)
		assert.Equal(t, "shared.com", result)
	})

	t.Run("falls back to legacy key", func(t *testing.T) {
		options := map[string]string{
			"allowed_origins": "legacy.com",
		}

		result := getOrigins(options)
		assert.Equal(t, "legacy.com", result)
	})

	t.Run("returns empty when no keys present", func(t *testing.T) {
		options := map[string]string{}

		result := getOrigins(options)
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
		assert.Equal(t, "allow_origins", sharedAllowOrigins)
		assert.Equal(t, "allowed_origins", legacyAllowedOrigins)
	})

	t.Run("topic constants defined", func(t *testing.T) {
		assert.Equal(t, "ws.message", MessageTopic)
		assert.Equal(t, "ws.join", JoinTopic)
		assert.Equal(t, "ws.leave", LeaveTopic)
		assert.Equal(t, "ws.control", ControlTopic)
		assert.Equal(t, "ws.close", CloseTopic)
		assert.Equal(t, "ws.heartbeat", HeartbeatTopic)
	})

	t.Run("header constant defined", func(t *testing.T) {
		assert.Equal(t, "X-WS-Relay", RelayHeader)
	})
}

func TestErrorFactories(t *testing.T) {
	t.Run("NewAttachToRelayError", func(t *testing.T) {
		cause := assert.AnError
		err := NewAttachToRelayError(cause)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "attach")
	})

	t.Run("NewTranscodeError", func(t *testing.T) {
		cause := assert.AnError
		err := NewTranscodeError(cause)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "transcode")
	})

	t.Run("NewMarshalError", func(t *testing.T) {
		cause := assert.AnError
		err := NewMarshalError("test object", cause)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "test object")
	})

	t.Run("NewWebSocketWriteError", func(t *testing.T) {
		cause := assert.AnError
		err := NewWebSocketWriteError(cause)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "WebSocket")
	})

	t.Run("NewMarshalJoinInfoError", func(t *testing.T) {
		cause := assert.AnError
		err := NewMarshalJoinInfoError(cause)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "join")
	})

	t.Run("NewMarshalLeaveInfoError", func(t *testing.T) {
		cause := assert.AnError
		err := NewMarshalLeaveInfoError(cause)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "leave")
	})
}

func TestSentinelErrors(t *testing.T) {
	t.Run("ErrHostRequired", func(t *testing.T) {
		assert.Error(t, ErrHostRequired)
		assert.Contains(t, ErrHostRequired.Error(), "host")
	})

	t.Run("ErrNodeRequired", func(t *testing.T) {
		assert.Error(t, ErrNodeRequired)
		assert.Contains(t, ErrNodeRequired.Error(), "node")
	})

	t.Run("ErrTranscoderRequired", func(t *testing.T) {
		assert.Error(t, ErrTranscoderRequired)
		assert.Contains(t, ErrTranscoderRequired.Error(), "transcoder")
	})

	t.Run("ErrFrameContextNotFound", func(t *testing.T) {
		assert.Error(t, ErrFrameContextNotFound)
		assert.Contains(t, ErrFrameContextNotFound.Error(), "FrameContext")
	})

	t.Run("ErrServerHostNotFound", func(t *testing.T) {
		assert.Error(t, ErrServerHostNotFound)
		assert.Contains(t, ErrServerHostNotFound.Error(), "host")
	})

	t.Run("ErrServerIDNotFound", func(t *testing.T) {
		assert.Error(t, ErrServerIDNotFound)
		assert.Contains(t, ErrServerIDNotFound.Error(), "ID")
	})

	t.Run("ErrInvalidServerID", func(t *testing.T) {
		assert.Error(t, ErrInvalidServerID)
		assert.Contains(t, ErrInvalidServerID.Error(), "server ID")
	})

	t.Run("ErrHostNotAttachable", func(t *testing.T) {
		assert.Error(t, ErrHostNotAttachable)
		assert.Contains(t, ErrHostNotAttachable.Error(), "AttachableHost")
	})

	t.Run("ErrExpectedBytesPayload", func(t *testing.T) {
		assert.Error(t, ErrExpectedBytesPayload)
		assert.Contains(t, ErrExpectedBytesPayload.Error(), "bytes")
	})
}
