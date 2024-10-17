package eventsbus

import (
	"github.com/ponyruntime/pony/api"
)

type E struct {
	// event type
	typ api.EventType
	// target subsystem
	subSystem api.SubSystem
	// content
	content any
}

// NewEvent initializes new event
// etype - event type
// subSystem - target subsystem
func NewEvent(etype api.EventType, subSystem api.SubSystem, content any) *E {
	if etype == "" || subSystem == "" {
		return nil
	}

	return &E{
		typ:       etype,
		subSystem: subSystem,
		content:   content,
	}
}

func (e *E) Type() api.EventType {
	return e.typ
}

func (e *E) SubSystem() api.SubSystem {
	return e.subSystem
}

func (e *E) Content() any {
	return e.content
}
