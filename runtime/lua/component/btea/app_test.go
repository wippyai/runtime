package btea

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// mockTranscoder implements payload.Transcoder for testing
type mockTranscoder struct {
	mock.Mock
}

func (m *mockTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	args := m.Called(p, format)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(payload.Payload), args.Error(1)
}

func (m *mockTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
	args := m.Called(p, v)
	return args.Error(0)
}

//// mockState implements the process state interface for testing
// type mockState struct {
//	mock.Mock
//	ctx context.Context
//}
//
// func (m *mockState) GetTaskCount() int {
//	args := m.Called()
//	return args.Int(0)
//}
//
// func (m *mockState) Step(block bool) error {
//	args := m.Called(block)
//	return args.Error(0)
//}
//
// func (m *mockState) SendPackage(pkg *pubsub.Package) error {
//	args := m.Called(pkg)
//	return args.Error(0)
//}
//
// func (m *mockState) Complete(err error, result interface{}) {
//	m.Called(err, result)
//}
//
// func (m *mockState) InitContext(ctx context.Context, pid pubsub.PID) error {
//	args := m.Called(ctx, pid)
//	m.ctx = ctx
//	return args.Error(0)
//}
//
// func (m *mockState) Start(input payload.Payloads, onStart func()) error {
//	args := m.Called(input, onStart)
//	return args.Error(0)
//}
//
// func (m *mockState) Ctx() context.Context {
//	return m.ctx
//}
//
// func (m *mockState) PID() pubsub.PID {
//	args := m.Called()
//	return args.Get(0).(pubsub.PID)
//}
//
// func (m *mockState) Log() *zap.Logger {
//	return zap.NewNop()
//}

func TestNewApp_NilTranscoder(t *testing.T) {
	log := zap.NewNop()
	runner := &engine.Runner{} // Use concrete type
	funcName := "test_func"

	app, err := NewApp(log, nil, runner, funcName)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transcoder is required")
	assert.Nil(t, app)
}

func TestApp_Start_NoTerminalContext(t *testing.T) {
	log := zap.NewNop()
	runner := &engine.Runner{} // Use concrete type
	transcoder := &mockTranscoder{}
	funcName := "test_func"

	app, _ := NewApp(log, transcoder, runner, funcName)

	ctx := context.Background()
	pid := pubsub.PID{
		Host:   "test_host",
		ID:     registry.ID{Name: "test_process"},
		UniqID: "test_uniq",
	}
	input := payload.Payloads{}

	err := app.Start(ctx, pid, input)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "terminal context not found")
}

func TestApp_Ready(t *testing.T) {
	log := zap.NewNop()
	runner := &engine.Runner{} // Use concrete type
	transcoder := &mockTranscoder{}
	funcName := "test_func"

	app, _ := NewApp(log, transcoder, runner, funcName)

	ready := app.Ready()

	// Should return 0 for uninitialized state
	assert.Equal(t, 0, ready)
}
