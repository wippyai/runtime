package process

import (
	"context"
	"github.com/ponyruntime/pony/api/payload"
)

// todo: command

type Process interface {
	Start(context.Context, payload.Payloads) error
	GetLayer(interface{}) any
	Step() error
	Stop() error
	IsCompleted() bool
	GetResult() payload.Payloads
}
