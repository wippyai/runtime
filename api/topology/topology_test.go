// Package topology provides process communication and lifecycle management.
package topology

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

func TestConstants(t *testing.T) {
	t.Run("ControlHost", func(t *testing.T) {
		assert.Equal(t, pid.HostID("node:control"), ControlHost)
	})

	t.Run("TopicInbox", func(t *testing.T) {
		assert.Equal(t, relay.Topic("@pid/inbox"), TopicInbox)
	})

	t.Run("TopicEvents", func(t *testing.T) {
		assert.Equal(t, relay.Topic("@pid/events"), TopicEvents)
	})
}

func TestKindConstants(t *testing.T) {
	tests := []struct {
		name     string
		kind     Kind
		expected string
	}{
		{"cancel", KindCancel, "pid.cancel"},
		{"exit", KindExit, "pid.exit"},
		{"link down", KindLinkDown, "pid.link.down"},
		{"monitor request", KindMonitorRequest, "pid.monitor.request"},
		{"monitor release", KindMonitorRelease, "pid.monitor.release"},
		{"link request", KindLinkRequest, "pid.link.request"},
		{"unlink request", KindUnlinkRequest, "pid.unlink.request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.kind)
		})
	}
}

func TestErrors(t *testing.T) {
	t.Run("name already registered", func(t *testing.T) {
		assert.Equal(t, "name already registered", ErrNameAlreadyRegistered.Error())
		assert.True(t, errors.Is(ErrNameAlreadyRegistered, ErrNameAlreadyRegistered))
	})

	t.Run("pid not found", func(t *testing.T) {
		assert.Equal(t, "pid not found", ErrPIDNotFound.Error())
		assert.Equal(t, "NotFound", ErrPIDNotFound.Kind().String())
	})

	t.Run("pid not registered", func(t *testing.T) {
		assert.Equal(t, "pid not registered", ErrPIDNotRegistered.Error())
		assert.Equal(t, "NotFound", ErrPIDNotRegistered.Kind().String())
	})

	t.Run("already monitoring", func(t *testing.T) {
		assert.Equal(t, "already monitoring pid", ErrAlreadyMonitoring.Error())
		assert.Equal(t, "AlreadyExists", ErrAlreadyMonitoring.Kind().String())
	})
}

func TestErrorInterface(t *testing.T) {
	err := ErrNameAlreadyRegistered
	assert.Equal(t, "AlreadyExists", err.Kind().String())
	assert.Equal(t, "False", err.Retryable().String())
	assert.Nil(t, err.Details())
}

func TestErrorMethods(t *testing.T) {
	t.Run("SetCause", func(t *testing.T) {
		cause := errors.New("underlying cause")
		newErr := apierror.SetCause(ErrPIDNotFound, cause)
		assert.True(t, errors.Is(newErr, cause))
		assert.Equal(t, ErrPIDNotFound.Error(), newErr.Error())
	})

	t.Run("SetDetails", func(t *testing.T) {
		bag := attrs.NewBagFrom(map[string]any{"pid": "test-pid"})
		newErr := apierror.SetDetails(ErrPIDNotFound, bag)
		assert.NotNil(t, newErr.Details())
	})

	t.Run("SetMessage", func(t *testing.T) {
		newErr := apierror.SetMessage(ErrPIDNotFound, "custom message")
		assert.Equal(t, "custom message", newErr.Error())
	})
}

func TestErrorIs(t *testing.T) {
	t.Run("same error", func(t *testing.T) {
		assert.True(t, errors.Is(ErrPIDNotFound, ErrPIDNotFound))
	})

	t.Run("wrapped error", func(t *testing.T) {
		cause := errors.New("cause")
		wrapped := apierror.SetCause(ErrPIDNotFound, cause)
		assert.True(t, errors.Is(wrapped, cause))
	})

	t.Run("different error type", func(t *testing.T) {
		stdErr := errors.New("standard error")
		assert.False(t, errors.Is(ErrPIDNotFound, stdErr))
	})
}

func TestExitEvent_Marshal(t *testing.T) {
	now := time.Now()

	event := ExitEvent{
		At:   now,
		Kind: KindExit,
		From: pid.PID{UniqID: "pid-123"},
		Result: &runtime.Result{
			Value: payload.New("result data"),
			Error: nil,
		},
	}

	data, err := json.Marshal(&event)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestCancelEvent_Marshal(t *testing.T) {
	now := time.Now()
	deadline := now.Add(5 * time.Second)

	event := CancelEvent{
		At:       now,
		Kind:     KindCancel,
		From:     pid.PID{UniqID: "pid-123"},
		Deadline: deadline,
	}

	data, err := json.Marshal(&event)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestCancel(t *testing.T) {
	from := pid.PID{UniqID: "from-pid"}
	to := pid.PID{UniqID: "to-pid"}
	deadline := time.Now().Add(10 * time.Second)

	pkg := Cancel(from, to, deadline)

	assert.NotNil(t, pkg)
	assert.Equal(t, to, pkg.Target)
	assert.Len(t, pkg.Messages, 1)
	assert.Equal(t, TopicEvents, pkg.Messages[0].Topic)

	assert.Equal(t, "topology", pkg.Source.UniqID)

	require.Len(t, pkg.Messages[0].Payloads, 1)
	event, ok := pkg.Messages[0].Payloads[0].Data().(*CancelEvent)
	require.True(t, ok)
	assert.Equal(t, KindCancel, event.Kind)
	assert.Equal(t, from.UniqID, event.From.UniqID)
	assert.WithinDuration(t, deadline, event.Deadline, time.Second)
}

func TestContext_Registry(t *testing.T) {
	t.Run("with app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		reg := GetRegistry(ctx)
		assert.Nil(t, reg)

		type mockRegistry struct{ PIDRegistry }
		mockReg := &mockRegistry{}

		ctx = WithRegistry(ctx, mockReg)

		retrieved := GetRegistry(ctx)
		assert.Equal(t, mockReg, retrieved)
	})

	t.Run("without app context", func(t *testing.T) {
		ctx := context.Background()

		reg := GetRegistry(ctx)
		assert.Nil(t, reg)

		type mockRegistry struct{ PIDRegistry }
		mockReg := &mockRegistry{}

		ctx = WithRegistry(ctx, mockReg)
		assert.Equal(t, context.Background(), ctx)

		reg = GetRegistry(ctx)
		assert.Nil(t, reg)
	})
}

func TestContext_Topology(t *testing.T) {
	t.Run("with app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		topo := GetTopology(ctx)
		assert.Nil(t, topo)

		type mockTopology struct{ Topology }
		mockTopo := &mockTopology{}

		ctx = WithTopology(ctx, mockTopo)

		retrieved := GetTopology(ctx)
		assert.Equal(t, mockTopo, retrieved)
	})

	t.Run("without app context", func(t *testing.T) {
		ctx := context.Background()

		topo := GetTopology(ctx)
		assert.Nil(t, topo)

		type mockTopology struct{ Topology }
		mockTopo := &mockTopology{}

		ctx = WithTopology(ctx, mockTopo)
		assert.Equal(t, context.Background(), ctx)

		topo = GetTopology(ctx)
		assert.Nil(t, topo)
	})
}
