package process

import (
	"context"
)

type (
	// ID uniquely identifies a process
	ID string

	Message interface {
		Kind() string
	}

	Process interface {
		ID() ID
		Run(ctx context.Context, msg chan<- []Message) (<-chan []Message, error)
	}
)
