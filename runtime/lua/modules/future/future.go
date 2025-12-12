package future

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"
	lua "github.com/yuin/gopher-lua"
)

const TypeName = "Future"

// CancelFunc is set by funcs package to provide cancel functionality.
// This avoids circular dependency while allowing cancel to be called.
var CancelFunc func(l *lua.LState) int

func init() {
	value.RegisterTypeMethods(nil, TypeName, nil, map[string]lua.LGoFunc{
		"await":       futureAwait,
		"response":    futureResponse,
		"channel":     futureResponse, // alias for backwards compatibility
		"is_complete": futureIsComplete,
		"is_canceled": futureIsCanceled,
		"result":      futureResult,
		"error":       futureError,
		"cancel":      futureCancel,
	})
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
	Topic   string
	Channel *engine.Channel

	mu        sync.Mutex
	completed bool
	canceled  bool
	result    lua.LValue
	err       error
}

// FutureAwaitYield wraps a ChannelResult with post-processing options.
// Implements HandledYield to handle result conversion on resume.
type FutureAwaitYield struct {
	Result        *engine.ChannelResult
	ReturnPayload bool
}

func (y *FutureAwaitYield) String() string       { return "<future_await_yield>" }
func (y *FutureAwaitYield) Type() lua.LValueType { return lua.LTUserData }

// GetChannelResult returns the wrapped ChannelResult for process channel handling.
func (y *FutureAwaitYield) GetChannelResult() *engine.ChannelResult {
	return y.Result
}

// HandleResult converts channel results to (value, error) format after yield resume.
func (y *FutureAwaitYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "")
		return []lua.LValue{lua.LNil, luaErr}
	}

	// data should be []lua.LValue from channel result
	results, ok := data.([]lua.LValue)
	if !ok || len(results) < 2 {
		luaErr := lua.NewLuaError(l, "invalid channel result").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}

	val := results[0]
	okVal := results[1]

	// Channel closed
	if okVal == lua.LFalse {
		luaErr := lua.NewLuaError(l, "channel closed").
			WithKind(lua.KindCanceled).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}

	// Check if value is a lua.Error (from handler)
	if luaErr, isErr := val.(*lua.Error); isErr {
		return []lua.LValue{lua.LNil, luaErr}
	}

	// Success - unwrap payload unless ReturnPayload is true
	return []lua.LValue{unwrapResult(l, val, y.ReturnPayload), lua.LNil}
}

// Ensure FutureAwaitYield implements HandledYield and ChannelResultProvider
var _ luaapi.HandledYield = (*FutureAwaitYield)(nil)
var _ engine.ChannelResultProvider = (*FutureAwaitYield)(nil)

// New creates a new Future with the given topic and channel.
func New(topic string, ch *engine.Channel) *Future {
	return &Future{
		Topic:   topic,
		Channel: ch,
	}
}

// CreateHandler returns a topic handler that processes async results.
func (f *Future) CreateHandler() engine.TopicHandler {
	return func(ctx context.Context, l *lua.LState, _ relay.PID, _ string, payloads []payload.Payload) lua.LValue {
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

// futureAwait blocks until the future completes and returns (value, error).
// Options: { payload = true } to return payload wrapper instead of unpacked Lua value.
func futureAwait(l *lua.LState) int {
	ud := l.CheckUserData(1)
	f, ok := ud.Value.(*Future)
	if !ok {
		l.ArgError(1, "Future expected")
		return 0
	}

	// Check for options table (second argument)
	returnPayload := false
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		options := l.CheckTable(2)
		if options.RawGetString("payload") == lua.LTrue {
			returnPayload = true
		}
	}

	// Check if already completed
	f.mu.Lock()
	if f.completed {
		if f.err != nil {
			luaErr := lua.WrapErrorWithLua(l, f.err, "")
			f.mu.Unlock()
			l.Push(lua.LNil)
			l.Push(luaErr)
			return 2
		}
		result := f.result
		f.mu.Unlock()
		l.Push(unwrapResult(l, result, returnPayload))
		l.Push(lua.LNil)
		return 2
	}
	f.mu.Unlock()

	// Block on channel
	result := f.Channel.Receive(l, nil)

	if result.Yields {
		// Wrap with FutureAwaitYield to handle result conversion on resume
		yield := &FutureAwaitYield{
			Result:        result,
			ReturnPayload: returnPayload,
		}
		l.Push(yield)
		return -1
	}

	// Non-blocking path (channel had value)
	return handleChannelResult(l, f, result, returnPayload)
}

// handleChannelResult converts channel result to (value, error) format.
func handleChannelResult(l *lua.LState, _ *Future, result *engine.ChannelResult, returnPayload bool) int {
	updates := result.GetUpdates()
	if len(updates) > 0 {
		res := updates[0]
		if res.Error != nil {
			engine.ReleaseResult(result)
			luaErr := lua.WrapErrorWithLua(l, res.Error, "")
			l.Push(lua.LNil)
			l.Push(luaErr)
			return 2
		}
		results := res.GetResult()
		engine.ReleaseResult(result)

		// Channel returns (value, ok)
		if len(results) >= 2 {
			val := results[0]
			okVal := results[1]

			// Channel closed
			if okVal == lua.LFalse {
				luaErr := lua.NewLuaError(l, "channel closed").
					WithKind(lua.KindCanceled).
					WithRetryable(false)
				l.Push(lua.LNil)
				l.Push(luaErr)
				return 2
			}

			// Check if value is a lua.Error (from handler)
			if luaErr, isErr := val.(*lua.Error); isErr {
				l.Push(lua.LNil)
				l.Push(luaErr)
				return 2
			}

			// Success - unwrap payload unless returnPayload is true
			l.Push(unwrapResult(l, val, returnPayload))
			l.Push(lua.LNil)
			return 2
		}
	}

	engine.ReleaseResult(result)
	luaErr := lua.NewLuaError(l, "invalid receive result").
		WithKind(lua.KindInternal).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(luaErr)
	return 2
}

// unwrapResult returns the payload wrapper or unpacked Lua value based on returnPayload flag.
func unwrapResult(l *lua.LState, val lua.LValue, returnPayload bool) lua.LValue {
	if returnPayload {
		return val
	}

	// Try to unwrap payload wrapper
	if ud, ok := val.(*lua.LUserData); ok {
		if pw, ok := ud.Value.(*payloadmod.Wrapper); ok {
			// Return the underlying Lua value
			if pw.Payload.Format() == payload.Lua {
				if lv, ok := pw.Payload.Data().(lua.LValue); ok {
					return lv
				}
			}
			// For non-Lua formats, transcode
			ctx := l.Context()
			if ctx != nil {
				tc := payload.GetTranscoder(ctx)
				if tc != nil {
					luaPayload, err := tc.Transcode(pw.Payload, payload.Lua)
					if err == nil {
						if lv, ok := luaPayload.Data().(lua.LValue); ok {
							return lv
						}
					}
				}
			}
		}
	}

	// Return as-is if not a payload wrapper
	return val
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
			WithKind(lua.KindCanceled).
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
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(luaErr)
		l.Push(lua.LTrue)
		return 2
	}
	l.Push(lua.LNil)
	l.Push(lua.LFalse)
	return 2
}
