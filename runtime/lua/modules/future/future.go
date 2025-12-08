package future

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const TypeName = "Future"

// CancelFunc is set by funcs package to provide cancel functionality.
// This avoids circular dependency while allowing cancel to be called.
var CancelFunc func(l *lua.LState) int

func init() {
	value.RegisterTypeMethods(nil, TypeName, nil, map[string]lua.LGoFunc{
		"await":       futureAwait,
		"channel":     futureChannel,
		"is_complete": futureIsComplete,
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
	result    lua.LValue
	err       error
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

		// Normal result
		f.result = engine.PayloadsToLua(ctx, l, payloads)
		return f.result
	}
}

// IsComplete returns true if the future has completed (non-blocking).
func (f *Future) IsComplete() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.completed
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
func futureAwait(l *lua.LState) int {
	ud := l.CheckUserData(1)
	f, ok := ud.Value.(*Future)
	if !ok {
		l.ArgError(1, "Future expected")
		return 0
	}

	// Check if already completed
	f.mu.Lock()
	if f.completed {
		if f.err != nil {
			// Preserve original kind/retryable from error chain
			luaErr := lua.WrapErrorWithLua(l, f.err, "")
			f.mu.Unlock()
			l.Push(lua.LNil)
			l.Push(luaErr)
			return 2
		}
		result := f.result
		f.mu.Unlock()
		l.Push(result)
		l.Push(lua.LNil)
		return 2
	}
	f.mu.Unlock()

	// Block on channel
	result := f.Channel.Receive(l, nil)

	if result.Yields {
		l.Push(result)
		return -1
	}

	// Non-blocking path (channel had value)
	return handleChannelResult(l, f, result)
}

// handleChannelResult converts channel result to (value, error) format.
func handleChannelResult(l *lua.LState, _ *Future, result *engine.ChannelResult) int {
	updates := result.GetUpdates()
	if len(updates) > 0 {
		res := updates[0]
		if res.Error != nil {
			engine.ReleaseResult(result)
			// Wrap error but preserve original kind/retryable from error chain
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

			// Success
			l.Push(val)
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

// futureChannel returns the underlying channel.
func futureChannel(l *lua.LState) int {
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

// futureResult returns (value, true) if completed successfully, (nil, false) otherwise.
func futureResult(l *lua.LState) int {
	ud := l.CheckUserData(1)
	f, ok := ud.Value.(*Future)
	if !ok {
		l.ArgError(1, "Future expected")
		return 0
	}

	if result, ok := f.Result(); ok {
		l.Push(result)
		l.Push(lua.LTrue)
		return 2
	}
	l.Push(lua.LNil)
	l.Push(lua.LFalse)
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
