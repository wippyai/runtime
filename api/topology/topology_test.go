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
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

func TestConstants(t *testing.T) {
	t.Run("ControlHost", func(t *testing.T) {
		assert.Equal(t, relay.HostID("node:control"), ControlHost)
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
		{"link established", KindLinkEstablished, "pid.link.established"},
		{"link removed", KindLinkRemoved, "pid.link.removed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.kind))
		})
	}
}

func TestErrors(t *testing.T) {
	t.Run("name already registered", func(t *testing.T) {
		assert.Equal(t, "name already registered", ErrNameAlreadyRegistered.Error())
		assert.True(t, errors.Is(ErrNameAlreadyRegistered, ErrNameAlreadyRegistered))
	})
}

func TestExitEvent_Marshal(t *testing.T) {
	now := time.Now()

	event := ExitEvent{
		At:   now,
		Kind: KindExit,
		From: relay.PID{UniqID: "pid-123"},
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
		From:     relay.PID{UniqID: "pid-123"},
		Deadline: deadline,
	}

	data, err := json.Marshal(&event)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestCancel(t *testing.T) {
	from := relay.PID{UniqID: "from-pid"}
	to := relay.PID{UniqID: "to-pid"}
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

func TestExit(t *testing.T) {
	pid := relay.PID{UniqID: "test-pid"}
	result := payload.New("exit result")
	testErr := errors.New("exit error")

	pkg := Exit(pid, result, testErr)

	assert.NotNil(t, pkg)
	assert.Equal(t, pid, pkg.Target)
	assert.Len(t, pkg.Messages, 1)
	assert.Equal(t, TopicEvents, pkg.Messages[0].Topic)

	assert.Equal(t, "topology", pkg.Source.UniqID)

	require.Len(t, pkg.Messages[0].Payloads, 1)
	event, ok := pkg.Messages[0].Payloads[0].Data().(*ExitEvent)
	require.True(t, ok)
	assert.Equal(t, KindExit, event.Kind)
	assert.Equal(t, pid.UniqID, event.From.UniqID)
	assert.NotNil(t, event.Result)
	assert.Equal(t, testErr, event.Result.Error)
}

func TestExit_NoError(t *testing.T) {
	pid := relay.PID{UniqID: "test-pid"}
	result := payload.New("success result")

	pkg := Exit(pid, result, nil)

	assert.NotNil(t, pkg)

	require.Len(t, pkg.Messages[0].Payloads, 1)
	event, ok := pkg.Messages[0].Payloads[0].Data().(*ExitEvent)
	require.True(t, ok)
	assert.Nil(t, event.Result.Error)
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
