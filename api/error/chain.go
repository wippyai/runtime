package error

import "errors"

// Chain is the serializable format for errors crossing process boundaries.
// Used by Temporal, queues, and other transports.
type Chain struct {
	Errors []ChainedError `json:"errors"`
}

// ChainedError represents one error in the chain.
type ChainedError struct {
	Message   string         `json:"msg"`
	Kind      string         `json:"kind,omitempty"`
	Retryable *bool          `json:"retry,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
	Stack     []string       `json:"stack,omitempty"` // ["file.lua:10 in foo", "main.lua:5"]
}

// StackProvider is implemented by errors that can provide stack traces.
type StackProvider interface {
	StackFrames() []string
}

// MessageProvider is implemented by errors that have a clean message
// separate from the full Error() string which may include wrapped errors.
type MessageProvider interface {
	Msg() string
}

// BuildChain walks an error chain and extracts metadata via interfaces.
// Works with any error implementing Kind(), Retryable(), Details(), StackFrames().
// Supports both apierror types and lua types (same semantics, different packages).
func BuildChain(err error) *Chain {
	if err == nil {
		return nil
	}

	chain := &Chain{Errors: make([]ChainedError, 0, 4)}

	for e := err; e != nil; {
		ce := ChainedError{Message: e.Error()}

		// Extract clean message if available
		if mp, ok := e.(MessageProvider); ok {
			ce.Message = mp.Msg()
		}

		// Extract Kind - try multiple interface patterns
		ce.Kind = extractKindString(e)

		// Extract Retryable - try multiple interface patterns
		ce.Retryable = extractRetryable(e)

		// Extract Details via interface
		if de, ok := e.(interface{ Details() map[string]any }); ok {
			if d := de.Details(); len(d) > 0 {
				ce.Details = d
			}
		}

		// Extract Stack via interface
		if se, ok := e.(StackProvider); ok {
			if s := se.StackFrames(); len(s) > 0 {
				ce.Stack = s
			}
		}

		chain.Errors = append(chain.Errors, ce)

		// Move to next in chain
		e = errors.Unwrap(e)
	}

	return chain
}

// extractKindString extracts Kind as string from error using various interface patterns.
func extractKindString(e error) string {
	// Try apierror.Kind return type (Kind has String() method)
	if ke, ok := e.(interface{ Kind() Kind }); ok {
		if s := ke.Kind().String(); s != "" && s != "Unknown" {
			return s
		}
	}

	// Try interface returning interface{} (for cross-package compatibility)
	if ke, ok := e.(interface {
		Kind() interface{ String() string }
	}); ok {
		if k := ke.Kind(); k != nil {
			if s := k.String(); s != "" && s != "Unknown" {
				return s
			}
		}
	}

	return ""
}

// extractRetryable extracts retryable flag from error using various interface patterns.
func extractRetryable(e error) *bool {
	// Try apierror.Ternary return type
	if re, ok := e.(interface{ Retryable() Ternary }); ok {
		r := re.Retryable()
		if r != Unspecified {
			b := r.Bool()
			return &b
		}
	}

	// Try interface returning interface{} with Bool() and String() (for lua.Ternary etc)
	if re, ok := e.(interface {
		Retryable() interface{ Bool() bool }
	}); ok {
		r := re.Retryable()
		if rt, ok := r.(interface{ String() string }); ok {
			if s := rt.String(); s != "Unspecified" && s != "Unknown" {
				b := r.Bool()
				return &b
			}
		}
	}

	return nil
}

// Root returns the first (outermost) error in the chain, or nil if empty.
func (c *Chain) Root() *ChainedError {
	if c == nil || len(c.Errors) == 0 {
		return nil
	}
	return &c.Errors[0]
}

// FromChain reconstructs a Rich error from a serialized chain.
func FromChain(chain *Chain) *RichError {
	if chain == nil || len(chain.Errors) == 0 {
		return nil
	}

	// Build from innermost to outermost
	var current *RichError
	for i := len(chain.Errors) - 1; i >= 0; i-- {
		ce := chain.Errors[i]

		e := &RichError{
			message:   ce.Message,
			kind:      Kind(ce.Kind),
			retryable: Unspecified,
		}

		if ce.Retryable != nil {
			if *ce.Retryable {
				e.retryable = True
			} else {
				e.retryable = False
			}
		}

		if len(ce.Details) > 0 {
			e.details = ce.Details
		}

		if len(ce.Stack) > 0 {
			e.stack = ce.Stack
		}

		if current != nil {
			e.cause = current
		}

		current = e
	}

	return current
}
