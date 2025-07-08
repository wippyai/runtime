package interceptor

import (
	"time"

	"github.com/ponyruntime/pony/api/event"
)

// Event system and kind constants
const (
	// System identifies the interceptor system in the event bus
	System event.System = "interceptor"

	// Register is sent to request registration of a new interceptor
	Register event.Kind = "interceptor.register"
	// Update is sent to request update of an existing interceptor
	Update event.Kind = "interceptor.update"
	// Delete is sent to request removal of an existing interceptor
	Delete event.Kind = "interceptor.delete"

	// Accept is sent when an interceptor registration is accepted
	Accept event.Kind = "interceptor.accept"
	// Reject is sent when an interceptor registration is rejected
	Reject event.Kind = "interceptor.reject"
)

// CommonOptions represents common execution options
type CommonOptions struct {
	Timeout     time.Duration
	RetryPolicy *RetryPolicy
	RateLimit   *RateLimit
}

// RetryPolicy defines retry behavior
type RetryPolicy struct {
	// MaxAttempts limits how many times to retry a failing operation
	MaxAttempts int
}

// RateLimit defines rate limiting behavior
type RateLimit struct {
	RequestsPerSecond int
	Burst             int
}
