// SPDX-License-Identifier: MPL-2.0

package error

import (
	"errors"

	"github.com/wippyai/runtime/api/attrs"
)

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

// BuildChain walks an error chain and extracts metadata via api/error interfaces.
// Errors that don't implement Error or Rich are serialized with message only.
// Convert domain errors to api/error before calling if you need kind/retryable/details.
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

		var richErr Rich
		if errors.As(e, &richErr) {
			kind := richErr.Kind()
			if kind != "" && kind != Unknown {
				ce.Kind = kind.String()
			}

			retryable := richErr.Retryable()
			if retryable != Unspecified {
				b := retryable.Bool()
				ce.Retryable = &b
			}

			if d := richErr.Details(); len(d) > 0 {
				ce.Details = d
			}

			if s := richErr.StackFrames(); len(s) > 0 {
				ce.Stack = s
			}
		} else {
			var baseErr Error
			if errors.As(e, &baseErr) {
				kind := baseErr.Kind()
				if kind != "" && kind != Unknown {
					ce.Kind = kind.String()
				}

				retryable := baseErr.Retryable()
				if retryable != Unspecified {
					b := retryable.Bool()
					ce.Retryable = &b
				}

				if d := baseErr.Details(); d != nil {
					if bag, ok := d.(attrs.Bag); ok && len(bag) > 0 {
						ce.Details = map[string]any(bag)
					}
				}
			} else {
				if se, ok := e.(StackProvider); ok {
					if s := se.StackFrames(); len(s) > 0 {
						ce.Stack = s
					}
				}
			}
		}

		chain.Errors = append(chain.Errors, ce)

		// Move to next in chain
		e = errors.Unwrap(e)
	}

	return chain
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
