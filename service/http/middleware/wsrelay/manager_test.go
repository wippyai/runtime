// SPDX-License-Identifier: MPL-2.0

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
