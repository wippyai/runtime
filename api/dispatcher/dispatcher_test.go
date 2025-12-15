package dispatcher

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
)

type mockCommand struct {
	id CommandID
}

func (m mockCommand) CmdID() CommandID { return m.id }

type mockHandler struct {
	handleCalled bool
	returnErr    error
}

func (m *mockHandler) Handle(ctx context.Context, cmd Command, tag uint64, receiver ResultReceiver) error {
	m.handleCalled = true
	return m.returnErr
}

type mockResultReceiver struct {
	tag  uint64
	data any
	err  error
}

func (m *mockResultReceiver) CompleteYield(tag uint64, data any, err error) {
	m.tag = tag
	m.data = data
	m.err = err
}

type mockRegistry struct {
	handlers map[CommandID]Handler
}

func (m *mockRegistry) Get(id CommandID) Handler {
	return m.handlers[id]
}

func (m *mockRegistry) Has(id CommandID) bool {
	_, ok := m.handlers[id]
	return ok
}

func (m *mockRegistry) Register(id CommandID, h Handler) {
	m.handlers[id] = h
}

func (m *mockRegistry) Dispatch(cmd Command) Handler {
	return m.handlers[cmd.CmdID()]
}

func TestHandlerFunc(t *testing.T) {
	var calledCtx context.Context
	var calledCmd Command
	var calledTag uint64
	var calledReceiver ResultReceiver

	fn := HandlerFunc(func(ctx context.Context, cmd Command, tag uint64, receiver ResultReceiver) error {
		calledCtx = ctx
		calledCmd = cmd
		calledTag = tag
		calledReceiver = receiver
		return nil
	})

	ctx := context.Background()
	cmd := mockCommand{id: 1}
	receiver := &mockResultReceiver{}

	err := fn.Handle(ctx, cmd, 42, receiver)

	assert.NoError(t, err)
	assert.Equal(t, ctx, calledCtx)
	assert.Equal(t, cmd, calledCmd)
	assert.Equal(t, uint64(42), calledTag)
	assert.Equal(t, receiver, calledReceiver)
}

func TestHandlerFunc_Error(t *testing.T) {
	expectedErr := errors.New("handler error")
	fn := HandlerFunc(func(ctx context.Context, cmd Command, tag uint64, receiver ResultReceiver) error {
		return expectedErr
	})

	err := fn.Handle(context.Background(), mockCommand{}, 0, nil)
	assert.Equal(t, expectedErr, err)
}

func TestWithRegistry(t *testing.T) {
	t.Run("with app context", func(t *testing.T) {
		ac := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), ac)

		reg := &mockRegistry{handlers: make(map[CommandID]Handler)}
		err := WithRegistry(ctx, reg)
		require.NoError(t, err)

		got := GetRegistry(ctx)
		assert.Equal(t, reg, got)
	})

	t.Run("without app context", func(t *testing.T) {
		ctx := context.Background()
		reg := &mockRegistry{handlers: make(map[CommandID]Handler)}

		err := WithRegistry(ctx, reg)
		assert.Equal(t, ctxapi.ErrNoAppContext, err)
	})
}

func TestGetRegistry(t *testing.T) {
	t.Run("no app context", func(t *testing.T) {
		ctx := context.Background()
		got := GetRegistry(ctx)
		assert.Nil(t, got)
	})

	t.Run("app context without registry", func(t *testing.T) {
		ac := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), ac)

		got := GetRegistry(ctx)
		assert.Nil(t, got)
	})

	t.Run("app context with wrong type", func(t *testing.T) {
		ac := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), ac)
		ac.With(registryCtxKey, "not a registry")

		got := GetRegistry(ctx)
		assert.Nil(t, got)
	})
}

func TestGetRegistrar(t *testing.T) {
	t.Run("no app context", func(t *testing.T) {
		ctx := context.Background()
		got := GetRegistrar(ctx)
		assert.Nil(t, got)
	})

	t.Run("with registrar", func(t *testing.T) {
		ac := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), ac)

		reg := &mockRegistry{handlers: make(map[CommandID]Handler)}
		ac.With(registryCtxKey, reg)

		got := GetRegistrar(ctx)
		assert.Equal(t, reg, got)
	})

	t.Run("app context with non-registrar", func(t *testing.T) {
		ac := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), ac)
		ac.With(registryCtxKey, "not a registrar")

		got := GetRegistrar(ctx)
		assert.Nil(t, got)
	})
}

func TestGetDispatcher(t *testing.T) {
	t.Run("no app context", func(t *testing.T) {
		ctx := context.Background()
		got := GetDispatcher(ctx)
		assert.Nil(t, got)
	})

	t.Run("with dispatcher", func(t *testing.T) {
		ac := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), ac)

		reg := &mockRegistry{handlers: make(map[CommandID]Handler)}
		ac.With(registryCtxKey, reg)

		got := GetDispatcher(ctx)
		assert.Equal(t, reg, got)
	})

	t.Run("app context with non-dispatcher", func(t *testing.T) {
		ac := ctxapi.NewAppContext()
		ctx := ctxapi.WithAppContext(context.Background(), ac)
		ac.With(registryCtxKey, "not a dispatcher")

		got := GetDispatcher(ctx)
		assert.Nil(t, got)
	})
}

func TestMockResultReceiver(t *testing.T) {
	receiver := &mockResultReceiver{}
	receiver.CompleteYield(123, "data", errors.New("test error"))

	assert.Equal(t, uint64(123), receiver.tag)
	assert.Equal(t, "data", receiver.data)
	assert.EqualError(t, receiver.err, "test error")
}
