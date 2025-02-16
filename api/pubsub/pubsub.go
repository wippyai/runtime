package pubsub

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

	Host interface {
		Sender
		Attach(PID, Receiver) (error, context.CancelFunc)
	}

	Sender interface {
		Send(ctx context.Context, pid PID, msg ...*Message) error
	}

	Receiver interface {
		Send(...*Message) error
	}
)
