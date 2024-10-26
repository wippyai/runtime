package eventsbus

import (
	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/payload"
)

type E struct {
	typ  api.EventType
	sub  api.Subsystem
	data payload.Payload
}

// NewEvent initializes new event.
func NewEvent(etype api.EventType, subsystem api.Subsystem, content payload.Payload) *E {
	if etype == "" || subsystem == "" {
		return nil
	}

	return &E{
		typ:  etype,
		sub:  subsystem,
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
