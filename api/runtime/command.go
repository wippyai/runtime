package runtime

import "github.com/ponyruntime/pony/api/context"

var CancellerCtx = &context.Key{Name: "runtime.canceller"}

type (
	ID   = string
	Type = string

	Canceller func(cmd Command)

	Command interface {
		ID() ID
		Type() Type
		Result() *Result
		Complete(result *Result) error
		Cancel() error
	}
)
