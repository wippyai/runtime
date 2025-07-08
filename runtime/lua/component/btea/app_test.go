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
