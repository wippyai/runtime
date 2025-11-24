package upstream

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
)

var (
	ErrRequestCompleted = errors.New("request already completed")
	ErrRequestCanceled  = errors.New("request canceled")

	requestCounter atomic.Uint64
)

// Request represents an asynchronous request (OUT + Response)
// Lua creates requests and sends them upstream, backend executes and responds
type Request struct {
	// Request identifier
	id      string
	reqType runtime.Type

	// Input parameters
	params []payload.Payload

	// Internal state
	mu        sync.Mutex
	completed bool
	canceled  bool
	result    *runtime.Result

	// Channel-related fields
	responseChannel *channel.Channel
	channelValue    lua.LValue
	unitOfWork      engine.UnitOfWork

	// Callback for cancellation
	onCancel runtime.Canceller
}

// NewRequest creates a new request
func NewRequest(l *lua.LState, reqType runtime.Type, onCancel runtime.Canceller, params ...payload.Payload) *Request {
	// Validate request type is not empty
	if reqType == "" {
		l.RaiseError("request type cannot be empty")
		return nil
	}

	id := fmt.Sprintf("req.%s.%d", reqType, requestCounter.Add(1))

	// Get unit of work from context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work context found")
		return nil
	}

	// Create response channel
	chanName := fmt.Sprintf("req.%s.%d", reqType, requestCounter.Load())
	respChan := channel.Named(chanName, 1)
	respValue := channel.Wrap(l, respChan)

	return &Request{
		id:              id,
		reqType:         reqType,
		params:          params,
		responseChannel: respChan,
		channelValue:    respValue,
		unitOfWork:      uw,
		onCancel:        onCancel,
	}
}

// ID returns the request's ID
func (r *Request) ID() runtime.ID {
	return r.id
}

// Type returns the request's type
func (r *Request) Type() runtime.Type {
	return r.reqType
}

// Params returns the request's parameters
func (r *Request) Params() payload.Payloads {
	return r.params
}

// Result returns the request's result
func (r *Request) Result() *runtime.Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.result
}

// Complete implements the runtime.Command interface, completing the request with a result
func (r *Request) Complete(result *runtime.Result) error {
	// Validate result is not nil
	if result == nil {
		return fmt.Errorf("result cannot be nil")
	}

	r.mu.Lock()

	if r.completed || r.canceled {
		r.mu.Unlock()
		return ErrRequestCompleted
	}

	r.completed = true
	r.result = result

	// Get a local reference to the channel
	respChan := r.responseChannel
	state := r.unitOfWork.State()

	r.mu.Unlock()

	// Send result (value or error) through channel before closing
	var sendValue lua.LValue
	if result.Error != nil {
		// Wrap error as a Lua error value
		sendValue = payloadmod.WrapPayload(state, payload.New(result.Error.Error()))
	} else if result.Value != nil {
		sendValue = payloadmod.WrapPayload(state, result.Value)
	} else {
		// Nil result
		sendValue = lua.LNil
	}

	err := channel.Send(state, respChan, sendValue)
	if err != nil {
		return err
	}

	return channel.Close(state, respChan)
}

// Cancel cancels the request
func (r *Request) Cancel() error {
	r.mu.Lock()

	if r.completed {
		r.mu.Unlock()
		return ErrRequestCompleted
	}

	if r.canceled {
		r.mu.Unlock()
		return nil // Already canceled
	}

	r.canceled = true
	r.result = &runtime.Result{
		Value: nil,
		Error: ErrRequestCanceled,
	}

	// Get local references
	respChan := r.responseChannel

	r.mu.Unlock()

	if r.onCancel != nil {
		r.onCancel(r)
	}

	return channel.Close(r.unitOfWork.State(), respChan)
}

// IsCompleted checks if the request has completed
func (r *Request) IsCompleted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.completed || r.canceled
}

// IsCanceled checks if the request was canceled
func (r *Request) IsCanceled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.canceled
}

// ResponseChannel returns the Lua channel value for receiving the response
func (r *Request) ResponseChannel() lua.LValue {
	return r.channelValue
}

// GetChannel returns the internal response channel for Go code to yield on
func (r *Request) GetChannel() *channel.Channel {
	return r.responseChannel
}

// SendAndYield sends a request to the upstream handler and yields on its response channel.
// This is a convenience function for workflow modules that need to send a request and
// immediately wait for its completion.
func SendAndYield(l *lua.LState, req *Request) int {
	upstream, ok := runtime.GetUpstream(l.Context())
	if !ok {
		l.RaiseError("no upstream handler found in context")
		return 0
	}

	if err := upstream.SendRequest(req); err != nil {
		l.RaiseError("failed to send request: %s", err.Error())
		return 0
	}

	return channel.Receive(l, req.responseChannel)
}
