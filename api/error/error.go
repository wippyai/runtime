// Package error provides error categorization and retry metadata.
package error

import "github.com/wippyai/runtime/api/attrs"

// Kind constants for error categorization.
const (
	KindUnknown          Kind = "Unknown"
	KindNotFound         Kind = "NotFound"
	KindAlreadyExists    Kind = "AlreadyExists"
	KindInvalid          Kind = "Invalid"
	KindPermissionDenied Kind = "PermissionDenied"
	KindUnavailable      Kind = "Unavailable"
	KindInternal         Kind = "Internal"
	KindCanceled         Kind = "Canceled"
	KindConflict         Kind = "Conflict"
	KindTimeout          Kind = "Timeout"
	KindRateLimited      Kind = "RateLimited"
)

// Ternary constants for retry decisions.
const (
	Unknown Ternary = iota
	True
	False
)

type (
	// Error extends the standard error interface with categorization and retry metadata.
	// Domains implement this interface to provide rich error information that can be
	// passed across layers (Go ↔ Lua, API ↔ Services, HTTP, Cluster).
	Error interface {
		error

		// Kind returns the error category for semantic handling.
		Kind() Kind

		// Retryable indicates if the operation should be retried.
		// Returns Unknown to defer decision to outer layers (composition pattern).
		Retryable() Ternary

		// Details returns structured metadata about the error.
		// Keys and values are domain-specific.
		Details() attrs.Attributes
	}

	// Kind categorizes errors semantically across all domains.
	Kind string

	// Ternary represents three-state logic for composable error handling.
	Ternary int
)

// String returns the string representation of Kind.
func (k Kind) String() string {
	return string(k)
}

// String returns the string representation of Ternary.
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

// Bool converts Ternary to boolean (Unknown becomes false).
func (t Ternary) Bool() bool {
	return t == True
}
