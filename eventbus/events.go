package eventsbus

import (
	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/payload"
)

type E struct {
	cmp     api.Component
	typ     api.EventType
	content payload.Payload
}

// NewEvent initializes new event.
func NewEvent(cmp api.Component, etype api.EventType, content payload.Payload) *E {
	if etype == "" || cmp == "" {
		return nil
	}

	return &E{
		cmp:     cmp,
		typ:     etype,
		content: content,
	}
}

func (e *E) Component() api.Component {
	return e.cmp
}

func (e *E) Type() api.EventType {
	return e.typ
}

func (e *E) Content() any {
	return e.content
}
