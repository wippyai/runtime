package eventsbus

import (
	"encoding/json"
	"github.com/ponyruntime/pony/api"
)

type E struct {
	typ  api.EventType
	sub  api.Subsystem
	data json.RawMessage
}

// NewEvent initializes new event.
func NewEvent(etype api.EventType, subsystem api.Subsystem, content []byte) *E {
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
