package runtime

import (
	"github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/payload"
)

//nolint:gochecknoglobals
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
