// Package runtime provides runtime execution and command management.
package runtime

import (
	"github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
)

var CancellerCtx = &context.Key{Name: "runtime.canceller"}

type (
	ID   = string
	Type = string

	Canceller func(cmd Command)

	Command interface {
		ID() ID
		Type() Type
		Params() payload.Payloads
		Result() *Result
		Complete(result *Result) error
		Cancel() error
	}
)
