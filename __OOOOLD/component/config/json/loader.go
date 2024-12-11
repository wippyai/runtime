package json

import (
	"encoding/json"
	"github.com/ponyruntime/pony/api/events"
	"os"

	"github.com/ponyruntime/pony/api/payload"
)

type Event struct {
	ComponentName events.System   `json:"components"`
	EventType     events.Kind     `json:"type"`
	Data          json.RawMessage `json:"payload"`
}

func (e Event) System() events.System {
	return e.ComponentName
}

func (e Event) Kind() events.Kind {
	return e.EventType
}

func (e Event) Payload() payload.Payload {
	return payload.NewJSON(e.Data)
}

func LoadChangelogFile(path string) ([]Event, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return LoadChangelog(file)
}

func LoadChangelog(data []byte) ([]Event, error) {
	var events []Event
	err := json.Unmarshal(data, &events)
	if err != nil {
		return nil, err
	}

	return events, nil
}
