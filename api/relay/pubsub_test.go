// Package relay provides message relay and routing system for inter-component communication.
package relay

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
)

func TestPID_String(t *testing.T) {
	t.Run("with node", func(t *testing.T) {
		pid := PID{Node: "node1", Host: "host1", UniqID: "proc1"}
		expected := "{node1@host1|proc1}"
		assert.Equal(t, expected, pid.String())
	})

	t.Run("without node", func(t *testing.T) {
		pid := PID{Host: "host1", UniqID: "proc1"}
		expected := "{host1|proc1}"
		assert.Equal(t, expected, pid.String())
	})

	t.Run("uses cached value", func(t *testing.T) {
		pid := PID{Host: "host1", UniqID: "proc1", cachedString: "{cached}"}
		assert.Equal(t, "{cached}", pid.String())
	})
}

func TestPID_Precomputed(t *testing.T) {
	pid := PID{Node: "node1", Host: "host1", UniqID: "proc1"}
	computed := pid.Precomputed()

	assert.Equal(t, pid.Node, computed.Node)
	assert.Equal(t, pid.Host, computed.Host)
	assert.Equal(t, pid.UniqID, computed.UniqID)
	assert.NotEmpty(t, computed.cachedString)
	assert.Equal(t, "{node1@host1|proc1}", computed.cachedString)
}

func TestParsePID(t *testing.T) {
	t.Run("with node", func(t *testing.T) {
		pid, err := ParsePID("{node1@host1|proc1}")
		require.NoError(t, err)
		assert.Equal(t, "node1", pid.Node)
		assert.Equal(t, "host1", pid.Host)
		assert.Equal(t, "proc1", pid.UniqID)
		assert.NotEmpty(t, pid.cachedString)
	})

	t.Run("without node", func(t *testing.T) {
		pid, err := ParsePID("{host1|proc1}")
		require.NoError(t, err)
		assert.Equal(t, "", pid.Node)
		assert.Equal(t, "host1", pid.Host)
		assert.Equal(t, "proc1", pid.UniqID)
	})

	t.Run("old 3-part format", func(t *testing.T) {
		pid, err := ParsePID("{node1@host1|ns:name|proc1}")
		require.NoError(t, err)
		assert.Equal(t, "node1", pid.Node)
		assert.Equal(t, "host1", pid.Host)
		assert.Equal(t, "proc1", pid.UniqID)
	})

	t.Run("missing braces", func(t *testing.T) {
		_, err := ParsePID("host1|proc1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing braces")
	})

	t.Run("missing opening brace", func(t *testing.T) {
		_, err := ParsePID("host1|proc1}")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing braces")
	})

	t.Run("missing closing brace", func(t *testing.T) {
		_, err := ParsePID("{host1|proc1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing braces")
	})

	t.Run("missing pipe", func(t *testing.T) {
		_, err := ParsePID("{host1proc1}")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing pipe")
	})

	t.Run("too short", func(t *testing.T) {
		_, err := ParsePID("{}")
		require.Error(t, err)
	})
}

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

	t.Run("release nil package", func(t *testing.T) {
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
func (m *mockHost) Attach(PID, chan *Package) (context.CancelFunc, error) {
	return func() {}, nil
}
func (m *mockHost) Detach(PID) {}

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
