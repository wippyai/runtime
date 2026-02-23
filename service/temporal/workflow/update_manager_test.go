// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/service/temporal/propagator"
	"go.uber.org/zap"
)

// mockUpdateCallbacks implements bindings.UpdateCallbacks for testing
type mockUpdateCallbacks struct {
	result    any
	err       error
	accepted  bool
	rejected  bool
	completed bool
}

func (m *mockUpdateCallbacks) Accept() {
	m.accepted = true
}

func (m *mockUpdateCallbacks) Reject(err error) {
	m.rejected = true
	m.err = err
}

func (m *mockUpdateCallbacks) Complete(result any, err error) {
	m.completed = true
	m.result = result
	m.err = err
}

func newTestUpdateManager() *UpdateManager {
	logger := zap.NewNop()
	replayLog := propagator.NewReplayLogger(logger, func() bool { return false })
	return NewUpdateManager(replayLog)
}

func TestNewUpdateManager(t *testing.T) {
	m := newTestUpdateManager()
	require.NotNil(t, m)
	assert.NotNil(t, m.active)
	assert.Empty(t, m.pending)
}

func TestUpdateManager_QueueUpdate(t *testing.T) {
	m := newTestUpdateManager()
	callbacks := &mockUpdateCallbacks{}

	m.QueueUpdate("test-update", "update-1", payload.Payloads{payload.NewString("data")}, callbacks)

	assert.True(t, m.HasPending())
	assert.Len(t, m.pending, 1)
	assert.Equal(t, "test-update", m.pending[0].Name)
	assert.Equal(t, "update-1", m.pending[0].ID)
	assert.Equal(t, updatePending, m.pending[0].State)
}

func TestUpdateManager_QueueRejection(t *testing.T) {
	m := newTestUpdateManager()
	callbacks := &mockUpdateCallbacks{}

	m.QueueRejection("update-1", "decode error", callbacks)

	assert.True(t, m.HasPending())
	assert.Len(t, m.pending, 1)
	assert.Equal(t, updateTopicReject, m.pending[0].Name)
}

func TestUpdateManager_HasPending(t *testing.T) {
	m := newTestUpdateManager()

	assert.False(t, m.HasPending())

	m.QueueUpdate("test", "id", nil, &mockUpdateCallbacks{})
	assert.True(t, m.HasPending())
}

func TestUpdateManager_FlushPending(t *testing.T) {
	t.Run("empty queue returns nil", func(t *testing.T) {
		m := newTestUpdateManager()
		result := m.FlushPending()
		assert.Nil(t, result)
	})

	t.Run("flushes normal updates to active", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}

		m.QueueUpdate("test", "update-1", nil, callbacks)
		result := m.FlushPending()

		assert.Len(t, result, 1)
		assert.Equal(t, "update-1", result[0].ID)
		assert.False(t, m.HasPending())
		assert.Contains(t, m.active, "update-1")
	})

	t.Run("rejects __reject__ updates immediately", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}

		m.QueueRejection("update-1", "error message", callbacks)
		result := m.FlushPending()

		assert.Len(t, result, 0)
		assert.True(t, callbacks.rejected)
		assert.Contains(t, callbacks.err.Error(), "error message")
		// pending is cleared after flush
		assert.False(t, m.HasPending())
	})

	t.Run("uses default error for empty rejection", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}

		upd := &updateEntry{
			Name:      updateTopicReject,
			ID:        "update-1",
			Payloads:  payload.Payloads{},
			State:     updatePending,
			Callbacks: callbacks,
		}
		m.pending = append(m.pending, upd)

		m.FlushPending()

		assert.True(t, callbacks.rejected)
		assert.Contains(t, callbacks.err.Error(), "update decode error")
	})
}

func TestUpdateManager_HandleResponse(t *testing.T) {
	t.Run("unknown update returns error", func(t *testing.T) {
		m := newTestUpdateManager()
		var resumeErr error
		m.HandleResponse("unknown-id", "ack", nil, func(data any, err error) {
			resumeErr = err
		})
		require.Error(t, resumeErr)
		assert.Contains(t, resumeErr.Error(), "unknown update")
	})

	t.Run("unknown topic returns error", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}
		m.active["update-1"] = &updateEntry{
			ID:        "update-1",
			State:     updatePending,
			Callbacks: callbacks,
		}

		var resumeErr error
		m.HandleResponse("update-1", "unknown-topic", nil, func(data any, err error) {
			resumeErr = err
		})
		require.Error(t, resumeErr)
		assert.Contains(t, resumeErr.Error(), "unknown update response")
	})

	t.Run("ack transitions to accepted", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}
		m.active["update-1"] = &updateEntry{
			ID:        "update-1",
			State:     updatePending,
			Callbacks: callbacks,
		}

		var resumeErr error
		m.HandleResponse("update-1", "ack", nil, func(data any, err error) {
			resumeErr = err
		})

		assert.NoError(t, resumeErr)
		assert.True(t, callbacks.accepted)
		assert.Equal(t, updateAccepted, m.active["update-1"].State)
	})

	t.Run("ack on non-pending fails", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}
		m.active["update-1"] = &updateEntry{
			ID:        "update-1",
			State:     updateAccepted,
			Callbacks: callbacks,
		}

		var resumeErr error
		m.HandleResponse("update-1", "ack", nil, func(data any, err error) {
			resumeErr = err
		})

		require.Error(t, resumeErr)
		assert.Contains(t, resumeErr.Error(), "already accepted")
	})

	t.Run("nak rejects and removes", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}
		m.active["update-1"] = &updateEntry{
			ID:        "update-1",
			State:     updatePending,
			Callbacks: callbacks,
		}

		var resumeErr error
		m.HandleResponse("update-1", "nak", payload.Payloads{payload.NewString("rejected")}, func(data any, err error) {
			resumeErr = err
		})

		assert.NoError(t, resumeErr)
		assert.True(t, callbacks.rejected)
		assert.Contains(t, callbacks.err.Error(), "rejected")
		assert.NotContains(t, m.active, "update-1")
	})

	t.Run("ok completes with result", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}
		m.active["update-1"] = &updateEntry{
			ID:        "update-1",
			State:     updateAccepted,
			Callbacks: callbacks,
		}
		resultPayload := payload.NewString("result")

		var resumeErr error
		m.HandleResponse("update-1", "ok", payload.Payloads{resultPayload}, func(data any, err error) {
			resumeErr = err
		})

		assert.NoError(t, resumeErr)
		assert.True(t, callbacks.completed)
		assert.Equal(t, resultPayload, callbacks.result)
		assert.NotContains(t, m.active, "update-1")
	})

	t.Run("ok on non-accepted fails", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}
		m.active["update-1"] = &updateEntry{
			ID:        "update-1",
			State:     updatePending,
			Callbacks: callbacks,
		}

		var resumeErr error
		m.HandleResponse("update-1", "ok", nil, func(data any, err error) {
			resumeErr = err
		})

		require.Error(t, resumeErr)
		assert.Contains(t, resumeErr.Error(), "not accepted")
	})

	t.Run("error completes with error", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}
		m.active["update-1"] = &updateEntry{
			ID:        "update-1",
			State:     updateAccepted,
			Callbacks: callbacks,
		}

		var resumeErr error
		m.HandleResponse("update-1", "error", payload.Payloads{payload.NewString("failed")}, func(data any, err error) {
			resumeErr = err
		})

		assert.NoError(t, resumeErr)
		assert.True(t, callbacks.completed)
		assert.Nil(t, callbacks.result)
		assert.Contains(t, callbacks.err.Error(), "failed")
		assert.NotContains(t, m.active, "update-1")
	})

	t.Run("error on non-accepted fails", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}
		m.active["update-1"] = &updateEntry{
			ID:        "update-1",
			State:     updatePending,
			Callbacks: callbacks,
		}

		var resumeErr error
		m.HandleResponse("update-1", "error", nil, func(data any, err error) {
			resumeErr = err
		})

		require.Error(t, resumeErr)
		assert.Contains(t, resumeErr.Error(), "not accepted")
	})
}

func TestUpdateManager_FlushPending_NonStringPayload(t *testing.T) {
	m := newTestUpdateManager()
	callbacks := &mockUpdateCallbacks{}

	upd := &updateEntry{
		Name:      updateTopicReject,
		ID:        "update-1",
		Payloads:  payload.Payloads{payload.New(42)},
		State:     updatePending,
		Callbacks: callbacks,
	}
	m.pending = append(m.pending, upd)

	m.FlushPending()

	assert.True(t, callbacks.rejected)
	// Non-string payload data is formatted via Sprintf
	assert.NotEmpty(t, callbacks.err.Error())
}

func TestUpdateManager_HandleResponse_OkEmptyPayloads(t *testing.T) {
	m := newTestUpdateManager()
	callbacks := &mockUpdateCallbacks{}
	m.active["update-1"] = &updateEntry{
		ID:        "update-1",
		State:     updateAccepted,
		Callbacks: callbacks,
	}

	var resumeErr error
	m.HandleResponse("update-1", "ok", nil, func(data any, err error) {
		resumeErr = err
	})

	assert.NoError(t, resumeErr)
	assert.True(t, callbacks.completed)
	assert.Nil(t, callbacks.result)
	assert.Nil(t, callbacks.err)
	assert.NotContains(t, m.active, "update-1")
}

func TestUpdateManager_HandleResponse_NakNonStringPayload(t *testing.T) {
	m := newTestUpdateManager()
	callbacks := &mockUpdateCallbacks{}
	m.active["update-1"] = &updateEntry{
		ID:        "update-1",
		State:     updatePending,
		Callbacks: callbacks,
	}

	var resumeErr error
	m.HandleResponse("update-1", "nak", payload.Payloads{payload.New(500)}, func(data any, err error) {
		resumeErr = err
	})

	assert.NoError(t, resumeErr)
	assert.True(t, callbacks.rejected)
	assert.NotEmpty(t, callbacks.err.Error())
	assert.NotContains(t, m.active, "update-1")
}

func TestUpdateManager_HandleResponse_ErrorNonStringPayload(t *testing.T) {
	m := newTestUpdateManager()
	callbacks := &mockUpdateCallbacks{}
	m.active["update-1"] = &updateEntry{
		ID:        "update-1",
		State:     updateAccepted,
		Callbacks: callbacks,
	}

	var resumeErr error
	m.HandleResponse("update-1", "error", payload.Payloads{payload.New(503)}, func(data any, err error) {
		resumeErr = err
	})

	assert.NoError(t, resumeErr)
	assert.True(t, callbacks.completed)
	assert.NotEmpty(t, callbacks.err.Error())
	assert.NotContains(t, m.active, "update-1")
}

func TestUpdateManager_HandleResponse_NakDefaultMessage(t *testing.T) {
	m := newTestUpdateManager()
	callbacks := &mockUpdateCallbacks{}
	m.active["update-1"] = &updateEntry{
		ID:        "update-1",
		State:     updatePending,
		Callbacks: callbacks,
	}

	var resumeErr error
	m.HandleResponse("update-1", "nak", nil, func(data any, err error) {
		resumeErr = err
	})

	assert.NoError(t, resumeErr)
	assert.True(t, callbacks.rejected)
	assert.Contains(t, callbacks.err.Error(), "update rejected")
}

func TestUpdateManager_HandleResponse_ErrorDefaultMessage(t *testing.T) {
	m := newTestUpdateManager()
	callbacks := &mockUpdateCallbacks{}
	m.active["update-1"] = &updateEntry{
		ID:        "update-1",
		State:     updateAccepted,
		Callbacks: callbacks,
	}

	var resumeErr error
	m.HandleResponse("update-1", "error", nil, func(data any, err error) {
		resumeErr = err
	})

	assert.NoError(t, resumeErr)
	assert.True(t, callbacks.completed)
	assert.Contains(t, callbacks.err.Error(), "update failed")
}

func TestUpdateManager_HandleResponse_WrongStateTransitions(t *testing.T) {
	t.Run("ack on rejected", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}
		m.active["update-1"] = &updateEntry{
			ID: "update-1", State: updateRejected, Callbacks: callbacks,
		}

		var resumeErr error
		m.HandleResponse("update-1", "ack", nil, func(data any, err error) {
			resumeErr = err
		})
		require.Error(t, resumeErr)
		assert.Contains(t, resumeErr.Error(), "already rejected")
	})

	t.Run("ack on completed", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}
		m.active["update-1"] = &updateEntry{
			ID: "update-1", State: updateComplete, Callbacks: callbacks,
		}

		var resumeErr error
		m.HandleResponse("update-1", "ack", nil, func(data any, err error) {
			resumeErr = err
		})
		require.Error(t, resumeErr)
		assert.Contains(t, resumeErr.Error(), "already completed")
	})

	t.Run("nak on accepted", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}
		m.active["update-1"] = &updateEntry{
			ID: "update-1", State: updateAccepted, Callbacks: callbacks,
		}

		var resumeErr error
		m.HandleResponse("update-1", "nak", nil, func(data any, err error) {
			resumeErr = err
		})
		require.Error(t, resumeErr)
		assert.Contains(t, resumeErr.Error(), "already accepted")
		assert.False(t, callbacks.rejected)
	})

	t.Run("ok on rejected", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}
		m.active["update-1"] = &updateEntry{
			ID: "update-1", State: updateRejected, Callbacks: callbacks,
		}

		var resumeErr error
		m.HandleResponse("update-1", "ok", nil, func(data any, err error) {
			resumeErr = err
		})
		require.Error(t, resumeErr)
		assert.Contains(t, resumeErr.Error(), "not accepted")
		assert.False(t, callbacks.completed)
	})

	t.Run("ok on completed", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}
		m.active["update-1"] = &updateEntry{
			ID: "update-1", State: updateComplete, Callbacks: callbacks,
		}

		var resumeErr error
		m.HandleResponse("update-1", "ok", nil, func(data any, err error) {
			resumeErr = err
		})
		require.Error(t, resumeErr)
		assert.Contains(t, resumeErr.Error(), "not accepted")
	})

	t.Run("error on rejected", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}
		m.active["update-1"] = &updateEntry{
			ID: "update-1", State: updateRejected, Callbacks: callbacks,
		}

		var resumeErr error
		m.HandleResponse("update-1", "error", nil, func(data any, err error) {
			resumeErr = err
		})
		require.Error(t, resumeErr)
		assert.Contains(t, resumeErr.Error(), "not accepted")
		assert.False(t, callbacks.completed)
	})

	t.Run("error on completed", func(t *testing.T) {
		m := newTestUpdateManager()
		callbacks := &mockUpdateCallbacks{}
		m.active["update-1"] = &updateEntry{
			ID: "update-1", State: updateComplete, Callbacks: callbacks,
		}

		var resumeErr error
		m.HandleResponse("update-1", "error", nil, func(data any, err error) {
			resumeErr = err
		})
		require.Error(t, resumeErr)
		assert.Contains(t, resumeErr.Error(), "not accepted")
	})
}

func TestUpdateManager_MultipleUpdates(t *testing.T) {
	m := newTestUpdateManager()
	cb1 := &mockUpdateCallbacks{}
	cb2 := &mockUpdateCallbacks{}

	m.QueueUpdate("update-a", "id-1", nil, cb1)
	m.QueueUpdate("update-b", "id-2", nil, cb2)

	result := m.FlushPending()
	assert.Len(t, result, 2)
	assert.Len(t, m.active, 2)

	// Process first update
	m.HandleResponse("id-1", "ack", nil, func(any, error) {})
	m.HandleResponse("id-1", "ok", nil, func(any, error) {})

	assert.Len(t, m.active, 1)
	assert.True(t, cb1.accepted)
	assert.True(t, cb1.completed)
	assert.False(t, cb2.accepted)
}
