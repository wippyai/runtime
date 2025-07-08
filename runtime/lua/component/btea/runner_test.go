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
