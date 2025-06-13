package interceptor

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/runtime"
)

type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(dur)
	return nil
}

type RetryOptions struct {
	MaxAttempts int `json:"attempts"`
}

type RateLimitOptions struct {
	RequestsPerSecond int `json:"rps"`
	Burst             int `json:"burst"`
}

type TimeoutOptions struct {
	Timeout Duration `json:"timeout"`
}

type Options struct {
	Retry     RetryOptions     `json:"retry,omitempty"`
	RateLimit RateLimitOptions `json:"ratelimit,omitempty"`
	Timeout   TimeoutOptions   `json:"timeout,omitempty"`
}

// Interceptor defines the interface for function execution interceptors
type Interceptor interface {
	// Handle processes the execution and calls next() to continue the chain
	Handle(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context)
}

// Registry interface provides access to the interceptor chain
type Registry interface {
	GetChain() Chain
}

// Chain represents a sequence of interceptors that can be executed in order
type Chain interface {
	// Execute executes the chain of interceptors
	Execute(ctx context.Context, f function.Func, task runtime.Task) (chan *runtime.Result, error)
}
