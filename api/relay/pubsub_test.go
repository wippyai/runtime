// Package relay provides message relay and routing system for inter-component communication.
package relay

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
)

func TestPackage_Pool(t *testing.T) {
	t.Run("acquire and release", func(t *testing.T) {
		p := AcquirePackage()
		require.NotNil(t, p)
		require.NotNil(t, p.Messages)

		p.Source = pid.PID{Host: "host1", UniqID: "proc1"}
		p.Target = pid.PID{Host: "host2", UniqID: "proc2"}
		p.Messages = append(p.Messages, &Message{Topic: "test"})

		ReleasePackage(p)

		assert.Empty(t, p.Source.Host)
		assert.Empty(t, p.Target.Host)
		assert.Empty(t, p.Messages)
	})

	t.Run("release nil package", func(_ *testing.T) {
		ReleasePackage(nil)
	})
}

func TestNewPackage(t *testing.T) {
	source := pid.PID{Host: "host1", UniqID: "proc1"}
	target := pid.PID{Host: "host2", UniqID: "proc2"}
	p1 := payload.New("test")
	p2 := payload.New(123)

	pkg := NewPackage(source, target, "test-topic", p1, p2)

	require.NotNil(t, pkg)
	assert.Equal(t, source, pkg.Source)
	assert.Equal(t, target, pkg.Target)
	require.Len(t, pkg.Messages, 1)
	assert.Equal(t, "test-topic", pkg.Messages[0].Topic)
	assert.Len(t, pkg.Messages[0].Payloads, 2)
}

func TestNewMessagePackage(t *testing.T) {
	source := pid.PID{Host: "host1", UniqID: "proc1"}
	target := pid.PID{Host: "host2", UniqID: "proc2"}
	msg1 := &Message{Topic: "topic1"}
	msg2 := &Message{Topic: "topic2"}

	pkg := NewMessagePackage(source, target, msg1, msg2)

	require.NotNil(t, pkg)
	assert.Equal(t, source, pkg.Source)
	assert.Equal(t, target, pkg.Target)
	require.Len(t, pkg.Messages, 2)
	assert.Equal(t, "topic1", pkg.Messages[0].Topic)
	assert.Equal(t, "topic2", pkg.Messages[1].Topic)
}

func TestPackage_AddMessage(t *testing.T) {
	pkg := &Package{}
	p1 := payload.New("data1")
	p2 := payload.New("data2")

	pkg.AddMessage("topic1", p1)
	pkg.AddMessage("topic2", p2)

	require.Len(t, pkg.Messages, 2)
	assert.Equal(t, "topic1", pkg.Messages[0].Topic)
	assert.Equal(t, "topic2", pkg.Messages[1].Topic)
	assert.Len(t, pkg.Messages[0].Payloads, 1)
	assert.Len(t, pkg.Messages[1].Payloads, 1)
}

type mockNode struct{}

func (m *mockNode) ID() pid.NodeID      { return "mock-node" }
func (m *mockNode) Send(*Package) error { return nil }
func (m *mockNode) Attach(pid.PID, chan *Package) (context.CancelFunc, error) {
	return func() {}, nil
}
func (m *mockNode) Detach(pid.PID)                          {}
func (m *mockNode) RegisterHost(pid.HostID, Receiver) error { return nil }
func (m *mockNode) UnregisterHost(pid.HostID)               {}
func (m *mockNode) GetHost(pid.HostID) (Receiver, bool)     { return nil, false }

type mockReceiver struct{}

func (m *mockReceiver) Send(*Package) error { return nil }

type mockNodeManager struct {
	node *mockNode
}

func (m *mockNodeManager) Node() Node                  { return m.node }
func (m *mockNodeManager) Start(context.Context) error { return nil }
func (m *mockNodeManager) Stop() error                 { return nil }

type mockHost struct{}

func (m *mockHost) Send(*Package) error { return nil }

func TestWithNode_GetNode(t *testing.T) {
	t.Run("returns nil when AppContext is nil", func(t *testing.T) {
		ctx := context.Background()
		node := GetNode(ctx)
		assert.Nil(t, node)
	})

	t.Run("returns nil when node not set", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
		node := GetNode(ctx)
		assert.Nil(t, node)
	})

	t.Run("sets and retrieves node", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
		mockN := &mockNode{}

		ctx = WithNode(ctx, mockN)
		node := GetNode(ctx)

		require.NotNil(t, node)
		assert.Equal(t, mockN, node)
	})

	t.Run("idempotent when already set", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())

		first := &mockNode{}
		ctx = WithNode(ctx, first)

		second := &mockNode{}
		ctx = WithNode(ctx, second)

		node := GetNode(ctx)
		assert.Equal(t, first, node)
	})

	t.Run("returns same context when AppContext is nil", func(t *testing.T) {
		ctx := context.Background()
		mockN := &mockNode{}
		newCtx := WithNode(ctx, mockN)
		assert.Equal(t, ctx, newCtx)
	})
}

func TestWithRouter_GetRouter(t *testing.T) {
	t.Run("returns nil when AppContext is nil", func(t *testing.T) {
		ctx := context.Background()
		router := GetRouter(ctx)
		assert.Nil(t, router)
	})

	t.Run("returns nil when router not set", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
		router := GetRouter(ctx)
		assert.Nil(t, router)
	})

	t.Run("sets and retrieves router", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
		mockR := &mockReceiver{}

		ctx = WithRouter(ctx, mockR)
		router := GetRouter(ctx)

		require.NotNil(t, router)
		assert.Equal(t, mockR, router)
	})

	t.Run("idempotent when already set", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())

		first := &mockReceiver{}
		ctx = WithRouter(ctx, first)

		second := &mockReceiver{}
		ctx = WithRouter(ctx, second)

		router := GetRouter(ctx)
		assert.Equal(t, first, router)
	})
}

func TestWithNodeManager_GetNodeManager(t *testing.T) {
	t.Run("returns nil when AppContext is nil", func(t *testing.T) {
		ctx := context.Background()
		nm := GetNodeManager(ctx)
		assert.Nil(t, nm)
	})

	t.Run("returns nil when nodeManager not set", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
		nm := GetNodeManager(ctx)
		assert.Nil(t, nm)
	})

	t.Run("sets and retrieves nodeManager", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
		mockNM := &mockNodeManager{node: &mockNode{}}

		ctx = WithNodeManager(ctx, mockNM)
		nm := GetNodeManager(ctx)

		require.NotNil(t, nm)
		assert.Equal(t, mockNM, nm)
	})

	t.Run("idempotent when already set", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())

		first := &mockNodeManager{node: &mockNode{}}
		ctx = WithNodeManager(ctx, first)

		second := &mockNodeManager{node: &mockNode{}}
		ctx = WithNodeManager(ctx, second)

		nm := GetNodeManager(ctx)
		assert.Equal(t, first, nm)
	})
}

func TestWithHost_GetHost(t *testing.T) {
	t.Run("returns nil when AppContext is nil", func(t *testing.T) {
		ctx := context.Background()
		host := GetHost(ctx)
		assert.Nil(t, host)
	})

	t.Run("returns nil when host not set", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
		host := GetHost(ctx)
		assert.Nil(t, host)
	})

	t.Run("sets and retrieves host", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
		mockH := &mockHost{}

		ctx = WithHost(ctx, mockH)
		host := GetHost(ctx)

		require.NotNil(t, host)
		assert.Equal(t, mockH, host)
	})

	t.Run("idempotent when already set", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())

		first := &mockHost{}
		ctx = WithHost(ctx, first)

		second := &mockHost{}
		ctx = WithHost(ctx, second)

		host := GetHost(ctx)
		assert.Equal(t, first, host)
	})
}

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      apierror.Error
		expected string
		kind     string
	}{
		{"ErrHostNotFound", ErrHostNotFound, "host not found", "NotFound"},
		{"ErrHostAlreadyExists", ErrHostAlreadyExists, "host already exists", "AlreadyExists"},
		{"ErrEmptyNodeID", ErrEmptyNodeID, "nodeID cannot be empty", "Invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
			assert.Equal(t, tt.kind, tt.err.Kind().String())
			assert.False(t, tt.err.Retryable().Bool())
		})
	}
}

func TestErrorMethods(t *testing.T) {
	t.Run("SetCause", func(t *testing.T) {
		cause := errors.New("underlying cause")
		newErr := apierror.SetCause(ErrHostNotFound, cause)
		assert.True(t, errors.Is(newErr, cause))
		assert.Equal(t, "host not found: underlying cause", newErr.Error())
	})

	t.Run("SetDetails", func(t *testing.T) {
		details := attrs.NewBagFrom(map[string]any{"key": "value"})
		newErr := apierror.SetDetails(ErrHostNotFound, details)
		assert.NotNil(t, newErr.Details())
		val, _ := newErr.Details().Get("key")
		assert.Equal(t, "value", val)
	})

	t.Run("SetMessage", func(t *testing.T) {
		newErr := apierror.SetMessage(ErrHostNotFound, "custom message")
		assert.Equal(t, "custom message", newErr.Error())
	})
}
