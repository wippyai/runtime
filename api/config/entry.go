package config

import (
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
)

const (
	Configuration events.System = "config"

	Create events.Kind = "entry.create"
	Update events.Kind = "entry.update"
	Delete events.Kind = "entry.delete"
	Accept events.Kind = "entry.accept"
	Reject events.Kind = "entry.reject"
)

type (
	StateID string

	ID       string
	Type     string
	Metadata map[string]any

	Entry struct {
		ID    ID
		Type  Type
		Meta  Metadata
		State payload.Payload
	}

	Action struct {
		Kind  events.Kind
		Entry Entry
	}

	Configurator interface {
		Apply(...Action) (StateID, error)
		Rollback(StateID) error
	}
)

// todo: pass transcoders
