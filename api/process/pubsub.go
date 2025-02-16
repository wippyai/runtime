package process

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/api/payload"
)

// todo: split?

var ErrAlreadyAttached = errors.New("receiver already attached")

type (
	Topic = string

	Message struct {
		Topic    Topic
		Payloads payload.Payloads
	}

	PubSub interface {
		Send(ctx context.Context, pid PID, msg ...*Message) error
		Attach(PID, Receiver) error
		Detach(PID) error
	}
)
