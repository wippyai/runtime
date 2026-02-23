// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

func TestErrorConstructors(t *testing.T) {
	t.Run("NewDriverNotFoundError", func(t *testing.T) {
		id := registry.NewID("test", "my-driver")
		err := NewDriverNotFoundError(id)
		assert.Contains(t, err.Error(), "driver not found")
		assert.Equal(t, apierror.NotFound, err.Kind())
		assert.Equal(t, apierror.False, err.Retryable())
		assert.NotNil(t, err.Details())
		val, ok := err.Details().Get("driver_id")
		assert.True(t, ok)
		assert.Equal(t, id.String(), val)
	})

	t.Run("NewQueueNotFoundError", func(t *testing.T) {
		id := registry.NewID("test", "my-queue")
		err := NewQueueNotFoundError(id)
		assert.Contains(t, err.Error(), "queue not found")
		assert.Equal(t, apierror.NotFound, err.Kind())
		val, ok := err.Details().Get("queue_id")
		assert.True(t, ok)
		assert.Equal(t, id.String(), val)
	})

	t.Run("NewDriverExistsError", func(t *testing.T) {
		id := registry.NewID("test", "my-driver")
		err := NewDriverExistsError(id)
		assert.Contains(t, err.Error(), "driver already exists")
		assert.Equal(t, apierror.AlreadyExists, err.Kind())
		val, ok := err.Details().Get("driver_id")
		assert.True(t, ok)
		assert.Equal(t, id.String(), val)
	})

	t.Run("NewQueueClosedError", func(t *testing.T) {
		id := registry.NewID("test", "my-queue")
		err := NewQueueClosedError(id)
		assert.Contains(t, err.Error(), "queue is closed")
		assert.Equal(t, apierror.Unavailable, err.Kind())
		val, ok := err.Details().Get("queue_id")
		assert.True(t, ok)
		assert.Equal(t, id.String(), val)
	})

	t.Run("NewConfigError", func(t *testing.T) {
		cause := errors.New("test error")
		err := NewConfigError("invalid config", cause)
		assert.Equal(t, "invalid config: test error", err.Error())
		assert.Equal(t, apierror.Invalid, err.Kind())
		assert.True(t, errors.Is(err, cause))
		assert.Equal(t, "test error", err.Details().GetString("cause", ""))
	})

	t.Run("NewUnsupportedKindError", func(t *testing.T) {
		err := NewUnsupportedKindError("unknown.kind")
		assert.Contains(t, err.Error(), "unsupported entry kind")
		assert.Equal(t, apierror.Invalid, err.Kind())
		val, ok := err.Details().Get("kind")
		assert.True(t, ok)
		assert.Equal(t, "unknown.kind", val)
	})

	t.Run("NewConcurrencyExceededError", func(t *testing.T) {
		err := NewConcurrencyExceededError(20, 10)
		assert.Equal(t, "concurrency exceeds maximum", err.Error())
		assert.Equal(t, apierror.Invalid, err.Kind())
		concVal, ok := err.Details().Get("concurrency")
		assert.True(t, ok)
		assert.Equal(t, 20, concVal)
		maxVal, ok := err.Details().Get("max")
		assert.True(t, ok)
		assert.Equal(t, 10, maxVal)
	})

	t.Run("NewPrefetchExceededError", func(t *testing.T) {
		err := NewPrefetchExceededError(100, 50)
		assert.Equal(t, "prefetch exceeds maximum", err.Error())
		assert.Equal(t, apierror.Invalid, err.Kind())
		prefVal, ok := err.Details().Get("prefetch")
		assert.True(t, ok)
		assert.Equal(t, 100, prefVal)
		maxVal, ok := err.Details().Get("max")
		assert.True(t, ok)
		assert.Equal(t, 50, maxVal)
	})
}

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     apierror.Error
		kind    apierror.Kind
		message string
	}{
		{"ErrDriverNotStarted", ErrDriverNotStarted, apierror.Unavailable, "queue driver not started"},
		{"ErrQueueFull", ErrQueueFull, apierror.Unavailable, "queue is full"},
		{"ErrQueueClosed", ErrQueueClosed, apierror.Unavailable, "queue is closed"},
		{"ErrConsumerClosed", ErrConsumerClosed, apierror.Unavailable, "consumer closed"},
		{"ErrNoPublishFunc", ErrNoPublishFunc, apierror.Unavailable, "no publish function configured"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.kind, tt.err.Kind())
			assert.Equal(t, tt.message, tt.err.Error())
		})
	}
}
