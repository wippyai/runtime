package eventsbus

import (
	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/payload"
)

type E struct {
	sub  api.Subsystem
	typ  api.EventType
	data payload.Payload
}

// NewEvent initializes new event.
func NewEvent(subsystem api.Subsystem, etype api.EventType, content payload.Payload) *E {
	if etype == "" || subsystem == "" {
		return nil
	}

	return &E{
		sub:  subsystem,
		typ:  etype,
		data: content,
	}
}

func (e *E) Type() api.EventType {
	return e.typ
}

func (e *E) Subsystem() api.Subsystem {
	return e.sub
}

func (e *E) Content() any {
	return e.data
}
