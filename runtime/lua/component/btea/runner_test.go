package btea

import (
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// mockUnitOfWork implements engine.UnitOfWork for testing
// type mockUnitOfWork struct {
//	mock.Mock
//}
//
// func (m *mockUnitOfWork) State() *lua.LState {
//	args := m.Called()
//	if args.Get(0) == nil {
//		return nil
//	}
//	return args.Get(0).(*lua.LState)
//}
//
// func (m *mockUnitOfWork) Values() engine.ValueStore {
//	args := m.Called()
//	return args.Get(0).(engine.ValueStore)
//}
//
// func (m *mockUnitOfWork) Tasks() engine.Tasks {
//	args := m.Called()
//	return args.Get(0).(engine.Tasks)
//}
//
// func (m *mockUnitOfWork) Context() context.Context {
//	args := m.Called()
//	return args.Get(0).(context.Context)
//}
//
// func (m *mockUnitOfWork) Run(fn func(engine.UnitOfWork)) {
//	m.Called(fn)
//}
//
// func (m *mockUnitOfWork) AddCleanup(fn func() error) context.CancelFunc {
//	args := m.Called(fn)
//	return args.Get(0).(context.CancelFunc)
//}
//
// func (m *mockUnitOfWork) Terminate(err error) error {
//	args := m.Called(err)
//	return args.Error(0)
//}
//
// func (m *mockUnitOfWork) Close() error {
//	args := m.Called()
//	return args.Error(0)
//}
//
//// mockTasks implements engine.Tasks for testing
// type mockTasks struct {
//	mock.Mock
//}
//
// func (m *mockTasks) Add() {
//	m.Called()
//}
//
// func (m *mockTasks) Done() {
//	m.Called()
//}
//
// func (m *mockTasks) WakeUp() {
//	m.Called()
//}
//
// func (m *mockTasks) Wait(ctx context.Context, block bool) ([]*engine.Update, error) {
//	args := m.Called(ctx, block)
//	if args.Get(0) == nil {
//		return nil, args.Error(1)
//	}
//	return args.Get(0).([]*engine.Update), args.Error(1)
//}
//
// func (m *mockTasks) Send(ctx context.Context, result *engine.Update) error {
//	args := m.Called(ctx, result)
//	return args.Error(0)
//}
//
// func (m *mockTasks) Schedule(fn func()) error {
//	args := m.Called(fn)
//	return args.Error(0)
//}
//
// func (m *mockTasks) Blocked() int {
//	args := m.Called()
//	return args.Int(0)
//}
//
// func (m *mockTasks) Ready() int {
//	args := m.Called()
//	return args.Int(0)
//}

// mockTranscoderRunner implements payload.Transcoder for testing
type mockTranscoderRunner struct {
	mock.Mock
}

func (m *mockTranscoderRunner) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	args := m.Called(p, format)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(payload.Payload), args.Error(1)
}

func (m *mockTranscoderRunner) Unmarshal(p payload.Payload, v interface{}) error {
	args := m.Called(p, v)
	return args.Error(0)
}

func TestTaskRunner_formatResult_NilPayload(t *testing.T) {
	log := zap.NewNop()
	runner := &TaskRunner{
		log: log,
	}

	result := runner.formatResult(nil)
	assert.Empty(t, result)
}

func TestTaskRunner_formatResult_LuaValue(t *testing.T) {
	log := zap.NewNop()
	runner := &TaskRunner{
		log: log,
	}

	// Create a Lua value payload
	luaValue := lua.LString("test string")
	payload := payload.NewPayload(luaValue, payload.Lua)

	result := runner.formatResult(payload)
	assert.Equal(t, "test string", result)
}

func TestTaskRunner_formatResult_WithTranscoder(t *testing.T) {
	log := zap.NewNop()
	transcoder := &mockTranscoderRunner{}
	runner := &TaskRunner{
		log: log,
		dtt: transcoder,
	}

	// Create a test payload
	testPayload := payload.NewPayload("test data", payload.String)
	stringPayload := payload.NewPayload("transcoded string", payload.String)

	// Setup transcoder mock
	transcoder.On("Transcode", testPayload, payload.String).Return(stringPayload, nil)

	result := runner.formatResult(testPayload)
	assert.Equal(t, "transcoded string", result)
	transcoder.AssertExpectations(t)
}

func TestTaskRunner_formatResult_TranscoderError(t *testing.T) {
	log := zap.NewNop()
	transcoder := &mockTranscoderRunner{}
	runner := &TaskRunner{
		log: log,
		dtt: transcoder,
	}

	// Create a test payload
	testPayload := payload.NewPayload("test data", payload.String)

	// Setup transcoder mock to return error
	transcoder.On("Transcode", testPayload, payload.String).Return(nil, assert.AnError)

	result := runner.formatResult(testPayload)
	assert.Equal(t, "non-string result", result)
	transcoder.AssertExpectations(t)
}

func TestTaskRunner_Close(t *testing.T) {
	log := zap.NewNop()
	runner := &TaskRunner{
		log: log,
	}

	// Create a mock Lua state and store it
	L := lua.NewState()
	defer L.Close()
	runner.state.Store(L)

	// Verify state is set
	assert.NotNil(t, runner.state.Load())

	// Close the runner
	runner.Close()

	// Verify state is cleared
	assert.Nil(t, runner.state.Load())
}

func TestTaskRunner_SendTask_NilState(t *testing.T) {
	log := zap.NewNop()
	runner := &TaskRunner{
		log: log,
	}

	// Ensure state is nil
	runner.state.Store(nil)

	input := lua.LString("test input")
	err := runner.SendTask("test_task", input)

	assert.Error(t, err)
	assert.Equal(t, process.ErrNoProcess, err)
}
