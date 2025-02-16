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
		Upstream
		Attach(PID, chan []*Message) (error, context.CancelFunc)
	}

	Upstream interface {
		Send(context.Context, PID, ...*Message) error
	}

	Downstream interface {
		Send(...*Message) error
	}
)
