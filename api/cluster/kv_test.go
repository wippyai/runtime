// SPDX-License-Identifier: MPL-2.0

package cluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestKVErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      apierror.Error
		expected string
		kind     apierror.Kind
	}{
		{"key not found", ErrKeyNotFound, "key not found", apierror.NotFound},
		{"lease not found", ErrLeaseNotFound, "lease not found", apierror.NotFound},
		{"lease expired", ErrLeaseExpired, "lease has expired", apierror.Invalid},
		{"version mismatch", ErrVersionMismatch, "version mismatch", apierror.Invalid},
		{"kv closed", ErrKVClosed, "kv is closed", apierror.Unavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
			assert.Equal(t, tt.kind, tt.err.Kind())
		})
	}

	t.Run("version mismatch is retryable", func(t *testing.T) {
		assert.Equal(t, apierror.True, ErrVersionMismatch.Retryable())
	})

	t.Run("key not found is not retryable", func(t *testing.T) {
		assert.Equal(t, apierror.False, ErrKeyNotFound.Retryable())
	})
}

func TestWatchEventType_Values(t *testing.T) {
	assert.Equal(t, WatchEventType(0), WatchPut)
	assert.Equal(t, WatchEventType(1), WatchDelete)
	assert.Equal(t, WatchEventType(2), WatchExpired)
}

func TestEntry(t *testing.T) {
	e := Entry{
		Key:     "/names/scheduler",
		Value:   []byte(`{"node":"node-1"}`),
		Version: 42,
		LeaseID: "lease-1",
	}

	assert.Equal(t, "/names/scheduler", e.Key)
	assert.Equal(t, Version(42), e.Version)
	assert.Equal(t, LeaseID("lease-1"), e.LeaseID)
}

func TestLeaseID(t *testing.T) {
	id := LeaseID("node-1-lease-42")
	assert.Equal(t, "node-1-lease-42", string(id))

	empty := LeaseID("")
	assert.Equal(t, "", string(empty))
}

func TestWatchEvent(t *testing.T) {
	current := &Entry{Key: "/names/scheduler", Value: []byte("node-2"), Version: 2}
	previous := &Entry{Key: "/names/scheduler", Value: []byte("node-1"), Version: 1}

	t.Run("put event with previous", func(t *testing.T) {
		ev := WatchEvent{Type: WatchPut, Current: current, Previous: previous}
		assert.Equal(t, WatchPut, ev.Type)
		assert.NotNil(t, ev.Current)
		assert.NotNil(t, ev.Previous)
	})

	t.Run("create event", func(t *testing.T) {
		ev := WatchEvent{Type: WatchPut, Current: current, Previous: nil}
		assert.Nil(t, ev.Previous)
	})

	t.Run("delete event", func(t *testing.T) {
		ev := WatchEvent{Type: WatchDelete, Current: nil, Previous: previous}
		assert.Nil(t, ev.Current)
		assert.NotNil(t, ev.Previous)
	})

	t.Run("expired event", func(t *testing.T) {
		ev := WatchEvent{Type: WatchExpired, Current: nil, Previous: previous}
		assert.Equal(t, WatchExpired, ev.Type)
	})
}
