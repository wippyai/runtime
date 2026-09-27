// SPDX-License-Identifier: MPL-2.0

package future

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"
)

func TestNew(t *testing.T) {
	ch := engine.NewChannel(1)
	f := New("test-topic", ch)

	if f.Topic != "test-topic" {
		t.Errorf("expected topic 'test-topic', got '%s'", f.Topic)
	}
	if f.Channel != ch {
		t.Error("channel not set correctly")
	}
	if f.IsComplete() {
		t.Error("new future should not be complete")
	}
}

func TestIsComplete(t *testing.T) {
	ch := engine.NewChannel(1)
	f := New("test", ch)

	if f.IsComplete() {
		t.Error("should not be complete initially")
	}

	f.mu.Lock()
	f.completed = true
	f.mu.Unlock()

	if !f.IsComplete() {
		t.Error("should be complete after setting")
	}
}

func TestResult(t *testing.T) {
	ch := engine.NewChannel(1)
	f := New("test", ch)

	// Not completed
	val, ok := f.Result()
	if ok || val != nil {
		t.Error("should return nil, false when not completed")
	}

	// Completed with result
	f.mu.Lock()
	f.completed = true
	f.result = lua.LString("test-result")
	f.mu.Unlock()

	val, ok = f.Result()
	if !ok {
		t.Error("should return true when completed with result")
	}
	if val != lua.LString("test-result") {
		t.Errorf("expected 'test-result', got %v", val)
	}
}

func TestResultWithError(t *testing.T) {
	ch := engine.NewChannel(1)
	f := New("test", ch)

	// Completed with error
	f.mu.Lock()
	f.completed = true
	f.err = errors.New("test error")
	f.mu.Unlock()

	val, ok := f.Result()
	if ok || val != nil {
		t.Error("should return nil, false when completed with error")
	}
}

func TestError(t *testing.T) {
	ch := engine.NewChannel(1)
	f := New("test", ch)

	// Not completed
	ok, err := f.Error()
	if ok || err != nil {
		t.Error("should return false, nil when not completed")
	}

	// Completed without error
	f.mu.Lock()
	f.completed = true
	f.result = lua.LString("result")
	f.mu.Unlock()

	ok, err = f.Error()
	if ok || err != nil {
		t.Error("should return false, nil when completed without error")
	}
}

func TestErrorWithError(t *testing.T) {
	ch := engine.NewChannel(1)
	f := New("test", ch)

	testErr := errors.New("test error")
	f.mu.Lock()
	f.completed = true
	f.err = testErr
	f.mu.Unlock()

	ok, err := f.Error()
	if !ok {
		t.Error("should return true when completed with error")
	}
	if !errors.Is(err, testErr) {
		t.Errorf("expected test error, got %v", err)
	}
}

func TestCreateHandler_Success(t *testing.T) {
	ch := engine.NewChannel(1)
	f := New("test", ch)
	handler := f.CreateHandler()

	l := lua.NewState()
	defer l.Close()

	// Simulate successful result
	p := payload.New(map[string]any{"key": "value"})
	result := handler(context.Background(), l, pid.PID{}, "", []payload.Payload{p})

	if result == nil {
		t.Error("handler should return a value")
	}
	if !f.IsComplete() {
		t.Error("future should be complete after handler")
	}
	if _, ok := f.Result(); !ok {
		t.Error("result should be available")
	}
}

func TestCreateHandler_Error(t *testing.T) {
	ch := engine.NewChannel(1)
	f := New("test", ch)
	handler := f.CreateHandler()

	l := lua.NewState()
	defer l.Close()

	// Simulate error result
	testErr := errors.New("async error")
	p := payload.NewError(testErr)
	result := handler(context.Background(), l, pid.PID{}, "", []payload.Payload{p})

	if result == nil {
		t.Error("handler should return error value")
	}
	if !f.IsComplete() {
		t.Error("future should be complete after handler")
	}
	if ok, err := f.Error(); !ok {
		t.Error("error should be available")
	} else if !errors.Is(err, testErr) {
		t.Errorf("expected test error, got %v", err)
	}
}

func TestCreateHandler_IgnoresDuplicate(t *testing.T) {
	ch := engine.NewChannel(1)
	f := New("test", ch)
	handler := f.CreateHandler()

	l := lua.NewState()
	defer l.Close()

	// First call
	p1 := payload.New("first")
	handler(context.Background(), l, pid.PID{}, "", []payload.Payload{p1})

	// Second call should be ignored
	p2 := payload.New("second")
	result := handler(context.Background(), l, pid.PID{}, "", []payload.Payload{p2})

	if result != nil {
		t.Error("duplicate call should return nil")
	}
}

func TestTypeMetatable(t *testing.T) {
	mt := value.GetTypeMetatable(nil, TypeName)
	if mt == nil {
		t.Fatal("Future type metatable not registered")
	}
}

func TestFutureIsComplete_Lua(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ch := engine.NewChannel(1)
	f := New("test", ch)

	ud := value.PushTypedUserData(l, f, TypeName)
	l.SetGlobal("future", ud)

	err := l.DoString(`
		local complete = future:is_complete()
		if complete then
			error("should not be complete")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestFutureResult_Lua(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ch := engine.NewChannel(1)
	f := New("test", ch)

	// Set completed with result
	f.mu.Lock()
	f.completed = true
	f.result = lua.LString("test-value")
	f.mu.Unlock()

	ud := value.PushTypedUserData(l, f, TypeName)
	l.SetGlobal("future", ud)

	err := l.DoString(`
		local val, err = future:result()
		if err then
			error("expected no error, got: " .. tostring(err))
		end
		if val ~= "test-value" then
			error("expected 'test-value', got " .. tostring(val))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestFutureResultPositionalContractOutputs(t *testing.T) {
	for _, arity := range []int{1, 2} {
		for _, fail := range []bool{false, true} {
			name := "success"
			if fail {
				name = "failure"
			}
			t.Run(string(rune('0'+arity))+"_"+name, func(t *testing.T) {
				l := lua.NewState()
				defer l.Close()
				f := New("contract", engine.NewChannel(1), arity)
				var values []payload.Payload
				if fail {
					values = []payload.Payload{payload.NewError(errors.New("failed"))}
				} else {
					values = []payload.Payload{payload.New("first")}
					if arity == 2 {
						values = append(values, payload.New("second"))
					}
				}
				f.CreateHandler()(context.Background(), l, pid.PID{}, "", values)
				l.SetGlobal("future", value.NewTypedUserData(l, f, TypeName))
				l.SetGlobal("expected_arity", lua.LInteger(arity))
				l.SetGlobal("expect_failure", lua.LBool(fail))
				err := l.DoString(`assert(select("#", future:result()) == expected_arity + 1)
					local results = {future:result()}
					if expect_failure then
						for i = 1, expected_arity do assert(results[i] == nil) end
						assert(type(results[expected_arity + 1]) == "userdata")
					else
						for i = 1, expected_arity do assert(type(results[i]) == "userdata") end
						assert(results[expected_arity + 1] == nil)
					end
					output_1, output_2, output_error = results[1], results[2], results[expected_arity + 1]`)
				require.NoError(t, err)
				if fail {
					_, ok := l.GetGlobal("output_error").(*lua.Error)
					require.True(t, ok)
				} else {
					first := l.GetGlobal("output_1").(*lua.LUserData).Value.(*payloadmod.Wrapper)
					require.Equal(t, "first", first.Payload.Data())
					if arity == 2 {
						second := l.GetGlobal("output_2").(*lua.LUserData).Value.(*payloadmod.Wrapper)
						require.Equal(t, "second", second.Payload.Data())
					}
				}
			})
		}
	}
}

func TestFutureError_Lua(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ch := engine.NewChannel(1)
	f := New("test", ch)

	// Set completed with error
	f.mu.Lock()
	f.completed = true
	f.err = errors.New("test error")
	f.mu.Unlock()

	ud := value.PushTypedUserData(l, f, TypeName)
	l.SetGlobal("future", ud)

	err := l.DoString(`
		local err, ok = future:error()
		if not ok then
			error("expected ok=true")
		end
		if err == nil then
			error("expected error")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestFutureChannel_Lua(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ch := engine.NewChannel(1)
	f := New("test", ch)

	ud := value.PushTypedUserData(l, f, TypeName)
	l.SetGlobal("future", ud)

	err := l.DoString(`
		local ch = future:response()
		if ch == nil then
			error("expected channel")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestCancelFunc_NotSet(t *testing.T) {
	// Save and restore CancelFunc
	original := CancelFunc
	CancelFunc = nil
	defer func() { CancelFunc = original }()

	l := lua.NewState()
	defer l.Close()

	ch := engine.NewChannel(1)
	f := New("test", ch)

	ud := value.PushTypedUserData(l, f, TypeName)
	l.SetGlobal("future", ud)

	err := l.DoString(`future:cancel()`)
	if err == nil {
		t.Error("expected error when CancelFunc is nil")
	}
}

func TestCancelFunc_Set(t *testing.T) {
	called := false
	CancelFunc = func(l *lua.LState) int {
		called = true
		l.Push(lua.LTrue)
		return 1
	}
	defer func() { CancelFunc = nil }()

	l := lua.NewState()
	defer l.Close()

	ch := engine.NewChannel(1)
	f := New("test", ch)

	ud := value.PushTypedUserData(l, f, TypeName)
	l.SetGlobal("future", ud)

	err := l.DoString(`future:cancel()`)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !called {
		t.Error("CancelFunc was not called")
	}
}

func TestConcurrentAccess(*testing.T) {
	ch := engine.NewChannel(1)
	f := New("test", ch)

	done := make(chan bool)

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			f.IsComplete()
			_, _ = f.Result()
			_, _ = f.Error()
			done <- true
		}()
	}

	// Concurrent write
	go func() {
		f.mu.Lock()
		f.completed = true
		f.result = lua.LString("done")
		f.mu.Unlock()
		done <- true
	}()

	for i := 0; i < 11; i++ {
		<-done
	}
}
