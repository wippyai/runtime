package error

// Error extends the standard error interface with categorization and retry metadata.
// Domains implement this interface to provide rich error information that can be
// passed across layers (Go ↔ Lua, API ↔ Services, HTTP, Cluster).
type Error interface {
	error

	// Kind returns the error category for semantic handling.
	Kind() Kind

	// Retryable indicates if the operation should be retried.
	// Returns Unknown to defer decision to outer layers (composition pattern).
	Retryable() Ternary

	// Details returns structured metadata about the error.
	// Keys and values are domain-specific.
	Details() map[string]any
}

// Kind categorizes errors semantically across all domains.
type Kind int

const (
	// KindUnknown is the default for uncategorized errors.
	KindUnknown Kind = iota

	// KindNotFound indicates a resource, key, token, policy, etc. was not found.
	// Examples: ErrKeyNotFound, ErrTokenNotFound, ErrResourceNotFound
	KindNotFound

	// KindAlreadyExists indicates a conflict with existing state.
	// Examples: ErrKeyExists, ErrConnectionClosed, ErrResourceReleased
	KindAlreadyExists

	// KindInvalid indicates validation failures or malformed input.
	// Examples: ErrInvalidKey, ValidationError, protocol errors
	KindInvalid

	// KindPermissionDenied indicates auth failures or locked resources.
	// Examples: ErrTokenExpired, ErrTokenRevoked, ErrResourceLocked
	KindPermissionDenied

	// KindUnavailable indicates temporary failures (network, capacity, throttling).
	// Examples: network errors, ErrStoreFull, connection issues
	KindUnavailable

	// KindInternal indicates unexpected system failures or bugs.
	// Should not be retried without investigation.
	KindInternal

	// KindCanceled indicates user or context cancellation.
	// Examples: context.Canceled, ErrTerminated
	KindCanceled

	// KindConflict indicates conflicts with concurrent operations.
	// Examples: ErrCAS (compare-and-swap), optimistic locking failures
	KindConflict

	// KindTimeout indicates operation exceeded time limit.
	// Examples: context.DeadlineExceeded, operation timeouts
	KindTimeout

	// KindRateLimited indicates throttling or rate limiting.
	// Examples: too many requests, quota exceeded
	KindRateLimited
)

// String returns the string representation of the Kind.
func (k Kind) String() string {
	switch k {
	case KindNotFound:
		return "NotFound"
	case KindAlreadyExists:
		return "AlreadyExists"
	case KindInvalid:
		return "Invalid"
	case KindPermissionDenied:
		return "PermissionDenied"
	case KindUnavailable:
		return "Unavailable"
	case KindInternal:
		return "Internal"
	case KindCanceled:
		return "Canceled"
	case KindConflict:
		return "Conflict"
	case KindTimeout:
		return "Timeout"
	case KindRateLimited:
		return "RateLimited"
	default:
		return "Unknown"
	}
}

// Ternary represents three-state logic for composable error handling.
// Enables layered retry decisions where inner layers can defer to outer layers.
type Ternary int

const (
	// Unknown defers the decision to outer layers.
	Unknown Ternary = iota

	// True indicates the operation should be retried.
	True

	// False indicates the operation should not be retried.
	False
)

// Bool converts Ternary to boolean (Unknown becomes false).
func (t Ternary) Bool() bool {
	return t == True
}

// String returns the string representation of the Ternary.
func (t Ternary) String() string {
	switch t {
	case True:
		return "True"
	case False:
		return "False"
	default:
		return "Unknown"
	}
}
