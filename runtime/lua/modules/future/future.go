// SPDX-License-Identifier: MPL-2.0

package future

import (
	"context"
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"
)

const TypeName = "Future"

// CancelFunc is set by funcs package to provide cancel functionality.
// This avoids circular dependency while allowing cancel to be called.
var CancelFunc func(l *lua.LState) int

// Methods is the set of Lua methods registered on the Future type. Exported so
// the type manifest (funcs.Future in the funcs package) can be checked against the
// methods the runtime actually registers.
var Methods = map[string]lua.LGoFunc{
	"response":    futureResponse,
	"channel":     futureResponse, // alias for backwards compatibility
	"is_complete": futureIsComplete,
	"is_canceled": futureIsCanceled,
	"result":      futureResult,
	"error":       futureError,
	"cancel":      futureCancel,
}

func init() {
	value.RegisterTypeMethods(nil, TypeName, nil, Methods)
}

// futureCancel delegates to CancelFunc if set.
func futureCancel(l *lua.LState) int {
	if CancelFunc != nil {
		return CancelFunc(l)
	}
	l.RaiseError("cancel not available")
	return 0
}

// Future represents an async operation that can be awaited.
type Future struct {
	result    lua.LValue
	err       error
	Channel   *engine.Channel
	Topic     string
	mu        sync.Mutex
	completed bool
	canceled  bool
}

// New creates a new Future with the given topic and channel.
func New(topic string, ch *engine.Channel) *Future {
	return &Future{
		Topic:   topic,
		Channel: ch,
	}
}

// CreateHandler returns a topic handler that processes async results.
func (f *Future) CreateHandler() engine.TopicHandler {
	return func(_ context.Context, l *lua.LState, _ pid.PID, _ string, payloads []payload.Payload) lua.LValue {
		f.mu.Lock()
		defer f.mu.Unlock()

		if f.completed {
			return nil
		}
		f.completed = true

		// Check for error payload
		if len(payloads) == 1 && payloads[0].Format() == payload.GoError {
			if err, ok := payloads[0].Data().(error); ok {
				f.err = err
				// Preserve original kind/retryable from error chain
				luaErr := lua.WrapErrorWithLua(l, err, "")
				return luaErr
			}
		}

		// Store result as payload wrapper (single payload) or table of payloads
		if len(payloads) == 1 {
			f.result = payloadmod.WrapPayload(l, payloads[0])
		} else if len(payloads) > 1 {
			tbl := l.CreateTable(len(payloads), 0)
			for i, pl := range payloads {
				tbl.RawSetInt(i+1, payloadmod.WrapPayload(l, pl))
			}
			f.result = tbl
		}
		return f.result
	}
}

// IsComplete returns true if the future has completed (non-blocking).
func (f *Future) IsComplete() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.completed
}

// IsCanceled returns true if cancel was called on this future.
func (f *Future) IsCanceled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.canceled
}

// MarkCanceled marks the future as canceled.
func (f *Future) MarkCanceled() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.canceled = true
}

// Result returns the cached result if completed successfully.
func (f *Future) Result() (lua.LValue, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.completed || f.err != nil {
		return nil, false
	}
	return f.result, true
}

// Error returns the cached error if completed with error.
func (f *Future) Error() (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.completed || f.err == nil {
		return false, nil
	}
	return true, f.err
}

// futureResponse returns the underlying channel.
func futureResponse(l *lua.LState) int {
	ud := l.CheckUserData(1)
	f, ok := ud.Value.(*Future)
	if !ok {
		l.ArgError(1, "Future expected")
		return 0
	}

	// If channel doesn't have lua value set yet, create and set it
	if f.Channel.Value() == nil {
		engine.PushChannel(l, f.Channel)
	} else {
		l.Push(f.Channel.Value())
	}
	return 1
}

// futureIsComplete returns true if the future has completed (non-blocking).
func futureIsComplete(l *lua.LState) int {
	ud := l.CheckUserData(1)
	f, ok := ud.Value.(*Future)
	if !ok {
		l.ArgError(1, "Future expected")
		return 0
	}

	if f.IsComplete() {
		l.Push(lua.LTrue)
	} else {
		l.Push(lua.LFalse)
	}
	return 1
}

// futureIsCanceled returns true if cancel was called on this future.
func futureIsCanceled(l *lua.LState) int {
	ud := l.CheckUserData(1)
	f, ok := ud.Value.(*Future)
	if !ok {
		l.ArgError(1, "Future expected")
		return 0
	}

	if f.IsCanceled() {
		l.Push(lua.LTrue)
	} else {
		l.Push(lua.LFalse)
	}
	return 1
}

// futureResult returns (value, error) - value on success, error if failed/canceled.
func futureResult(l *lua.LState) int {
	ud := l.CheckUserData(1)
	f, ok := ud.Value.(*Future)
	if !ok {
		l.ArgError(1, "Future expected")
		return 0
	}

	// Check if canceled
	if f.IsCanceled() {
		luaErr := lua.NewLuaError(l, "canceled").
			WithKind(lua.Canceled).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	// Check if completed with error
	if hasErr, err := f.Error(); hasErr {
		luaErr := lua.WrapErrorWithLua(l, err, "")
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	// Check if completed successfully
	if result, ok := f.Result(); ok {
		l.Push(result)
		l.Push(lua.LNil)
		return 2
	}

	// Not completed yet
	l.Push(lua.LNil)
	l.Push(lua.LNil)
	return 2
}

// futureError returns (error, true) if completed with error, (nil, false) otherwise.
func futureError(l *lua.LState) int {
	ud := l.CheckUserData(1)
	f, ok := ud.Value.(*Future)
	if !ok {
		l.ArgError(1, "Future expected")
		return 0
	}

	if ok, err := f.Error(); ok {
		luaErr := lua.WrapErrorWithLua(l, err, "async call failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(luaErr)
		l.Push(lua.LTrue)
		return 2
	}
	l.Push(lua.LNil)
	l.Push(lua.LFalse)
	return 2
}
