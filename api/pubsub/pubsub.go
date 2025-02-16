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

	Batch = []*Message

	Host interface {
		Upstream
		Attach(PID, chan *Batch) (error, context.CancelFunc)
	}

	Upstream interface {
		Send(context.Context, PID, *Batch) error
	}

	Downstream interface {
		Send(*Batch) error
	}
)
