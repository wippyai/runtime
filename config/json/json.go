package json

import (
	"encoding/json"
	"github.com/ponyruntime/pony/api"
	"github.com/ponyruntime/pony/api/payload"
	"os"
)

type Event struct {
	ComponentName api.Component   `json:"component"`
	EventType     api.EventType   `json:"type"`
	Data          json.RawMessage `json:"payload"`
}

func (e Event) Component() api.Component {
	return e.ComponentName
}

func (e Event) Kind() api.EventType {
	return e.EventType
}

func (e Event) Payload() payload.Payload {
	return payload.NewJSON(e.Data)
}

func ReadChangelog(path string) ([]Event, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var events []Event
	err = json.Unmarshal(file, &events)
	if err != nil {
		return nil, err
	}

	return events, nil
}
