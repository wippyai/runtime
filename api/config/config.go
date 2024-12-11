package config

import (
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
)

const (
	EntryCreate events.Kind = "entry.create"
	EntryUpdate events.Kind = "entry.update"
	EntryDelete events.Kind = "entry.delete"
)

type (
	StateID string

	Entry struct {
		Component string
		Metadata  map[string]string
		Payload   payload.Payload
	}

	Action struct {
		Kind events.Kind
		Entry
	}

	Configurator interface {
		Apply(...Action) (StateID, error)
		Rollback(StateID) error
	}
)

func CreateEntry(e Entry) Action {
	return Action{
		Kind:  EntryCreate,
		Entry: e,
	}
}

func UpdateEntry(e Entry) Action {
	return Action{
		Kind:  EntryUpdate,
		Entry: e,
	}
}

func DeleteEntry(e Entry) Action {
	return Action{
		Kind:  EntryDelete,
		Entry: e,
	}
}
