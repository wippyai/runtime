package eventsbus

import (
	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/api/payload"
)

type E struct {
	cmp     api.Component
	typ     api.EventType
	payload payload.Payload
}

// NewEvent initializes new event.
func NewEvent(component api.Component, etype api.EventType, payload payload.Payload) *E {
	if etype == "" || component == "" {
		return nil
	}

	return &E{
		cmp:     component,
		typ:     etype,
		payload: payload,
	}
}

func (e *E) Component() api.Component {
	return e.cmp
}

func (e *E) Kind() api.EventType {
	return e.typ
}

func (e *E) Payload() payload.Payload {
	return e.payload
}
