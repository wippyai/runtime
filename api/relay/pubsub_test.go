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
	"github.com/wippyai/runtime/api/payload"
)

func TestPackage_Pool(t *testing.T) {
	t.Run("acquire and release", func(t *testing.T) {
		p := AcquirePackage()
		require.NotNil(t, p)
		require.NotNil(t, p.Messages)

		p.Source = PID{Host: "host1", UniqID: "proc1"}
		p.Target = PID{Host: "host2", UniqID: "proc2"}
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
	source := PID{Host: "host1", UniqID: "proc1"}
	target := PID{Host: "host2", UniqID: "proc2"}
	p1 := payload.New("test")
	p2 := payload.New(123)

	pkg := NewPackage(source, target, "test-topic", p1, p2)

	require.NotNil(t, pkg)
	assert.Equal(t, source, pkg.Source)
	assert.Equal(t, target, pkg.Target)
	require.Len(t, pkg.Messages, 1)
	assert.Equal(t, Topic("test-topic"), pkg.Messages[0].Topic)
	assert.Len(t, pkg.Messages[0].Payloads, 2)
}

func TestNewMessagePackage(t *testing.T) {
	source := PID{Host: "host1", UniqID: "proc1"}
	target := PID{Host: "host2", UniqID: "proc2"}
	msg1 := &Message{Topic: "topic1"}
	msg2 := &Message{Topic: "topic2"}

	pkg := NewMessagePackage(source, target, msg1, msg2)

	require.NotNil(t, pkg)
	assert.Equal(t, source, pkg.Source)
	assert.Equal(t, target, pkg.Target)
	require.Len(t, pkg.Messages, 2)
	assert.Equal(t, Topic("topic1"), pkg.Messages[0].Topic)
	assert.Equal(t, Topic("topic2"), pkg.Messages[1].Topic)
}

func TestPackage_AddMessage(t *testing.T) {
	pkg := &Package{}
	p1 := payload.New("data1")
	p2 := payload.New("data2")

	pkg.AddMessage("topic1", p1)
	pkg.AddMessage("topic2", p2)

	require.Len(t, pkg.Messages, 2)
	assert.Equal(t, Topic("topic1"), pkg.Messages[0].Topic)
	assert.Equal(t, Topic("topic2"), pkg.Messages[1].Topic)
	assert.Len(t, pkg.Messages[0].Payloads, 1)
	assert.Len(t, pkg.Messages[1].Payloads, 1)
}

type mockNode struct{}

func (m *mockNode) ID() NodeID          { return "mock-node" }
func (m *mockNode) Send(*Package) error { return nil }
func (m *mockNode) Attach(PID, chan *Package) (context.CancelFunc, error) {
	return func() {}, nil
}
func (m *mockNode) Detach(PID)                      {}
func (m *mockNode) RegisterHost(HostID, Host) error { return nil }
func (m *mockNode) UnregisterHost(HostID)           {}
func (m *mockNode) GetHost(HostID) (Host, bool)     { return nil, false }

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
		err      *Error
		expected string
		kind     string
	}{
		{"ErrAlreadyAttached", ErrAlreadyAttached, "receiver already attached", "AlreadyExists"},
		{"ErrHostNotFound", ErrHostNotFound, "host not found", "NotFound"},
		{"ErrHostAlreadyExists", ErrHostAlreadyExists, "host already exists", "AlreadyExists"},
		{"ErrInvalidPIDFormat", ErrInvalidPIDFormat, "invalid pid format", "Invalid"},
		{"ErrNilPackage", ErrNilPackage, "cannot send nil package", "Invalid"},
		{"ErrEmptyNodeID", ErrEmptyNodeID, "nodeID cannot be empty", "Invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
			assert.Equal(t, tt.kind, tt.err.Kind().String())
			assert.False(t, tt.err.Retryable().Bool())
			assert.Nil(t, tt.err.Unwrap())
		})
	}
}

func TestErrorMethods(t *testing.T) {
	t.Run("WithCause", func(t *testing.T) {
		cause := errors.New("underlying cause")
		newErr := ErrHostNotFound.WithCause(cause)
		assert.Equal(t, cause, newErr.Unwrap())
		assert.Equal(t, ErrHostNotFound.Error(), newErr.Error())
	})

	t.Run("WithDetails", func(t *testing.T) {
		details := attrs.NewBagFrom(map[string]any{"key": "value"})
		newErr := ErrHostNotFound.WithDetails(details)
		assert.NotNil(t, newErr.Details())
		val, _ := newErr.Details().Get("key")
		assert.Equal(t, "value", val)
	})

	t.Run("WithMessage", func(t *testing.T) {
		newErr := ErrHostNotFound.WithMessage("custom message")
		assert.Equal(t, "custom message", newErr.Error())
	})
}

func TestErrorConstructors(t *testing.T) {
	t.Run("NewHostExistsError", func(t *testing.T) {
		err := NewHostExistsError("host1", "node1")
		assert.Contains(t, err.Error(), "host1")
		assert.Contains(t, err.Error(), "already exists")
		assert.Equal(t, "AlreadyExists", err.Kind().String())
		details := err.Details()
		require.NotNil(t, details)
		hostID, _ := details.Get("host_id")
		assert.Equal(t, "host1", hostID)
	})

	t.Run("NewHostNotFoundError", func(t *testing.T) {
		err := NewHostNotFoundError("host1", "node1")
		assert.Contains(t, err.Error(), "host1")
		assert.Contains(t, err.Error(), "not found")
		assert.Equal(t, "NotFound", err.Kind().String())
	})

	t.Run("NewInvalidHostTypeError", func(t *testing.T) {
		err := NewInvalidHostTypeError("host1", "node1")
		assert.Contains(t, err.Error(), "invalid type")
		assert.Equal(t, "Internal", err.Kind().String())
	})

	t.Run("NewExternalNodeError", func(t *testing.T) {
		err := NewExternalNodeError("node1")
		assert.Contains(t, err.Error(), "cannot route to external node")
		assert.Equal(t, "Unavailable", err.Kind().String())
	})

	t.Run("NewNodeNotFoundError", func(t *testing.T) {
		err := NewNodeNotFoundError("node1")
		assert.Contains(t, err.Error(), "not found")
		assert.Equal(t, "NotFound", err.Kind().String())
	})

	t.Run("NewHostNotAttachableError", func(t *testing.T) {
		err := NewHostNotAttachableError("host1")
		assert.Contains(t, err.Error(), "does not support attachment")
		assert.Equal(t, "Invalid", err.Kind().String())
	})

	t.Run("NewPeerExistsError", func(t *testing.T) {
		err := NewPeerExistsError("node1")
		assert.Contains(t, err.Error(), "peer node already registered")
		assert.Equal(t, "AlreadyExists", err.Kind().String())
	})

	t.Run("NewPeerConflictError", func(t *testing.T) {
		err := NewPeerConflictError("node1")
		assert.Contains(t, err.Error(), "conflicts with local node")
		assert.Equal(t, "Conflict", err.Kind().String())
	})

	t.Run("NewSubscriberError", func(t *testing.T) {
		cause := errors.New("subscriber error")
		err := NewSubscriberError(cause)
		assert.Contains(t, err.Error(), "failed to create subscriber")
		assert.True(t, err.Retryable().Bool())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("NewAlreadyAttachedError", func(t *testing.T) {
		pid := PID{Host: "host1", UniqID: "proc1"}
		err := NewAlreadyAttachedError(pid)
		assert.Contains(t, err.Error(), "already attached")
		assert.Equal(t, ErrAlreadyAttached, err.Unwrap())
	})
}
