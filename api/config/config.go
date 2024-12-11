package config

import (
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
)

const (
	Create events.Kind = "entry.create"
	Update events.Kind = "entry.update"
	Delete events.Kind = "entry.delete"
	Accept events.Kind = "entry.accept"
	Reject events.Kind = "entry.reject"
)

type (
	StateID string

	Entry struct {
		Component string
		Metadata  map[string]string
		Payload   payload.Payload
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
