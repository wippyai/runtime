package events

import (
	"github.com/ponyruntime/pony/api/payload"
	"strings"
)

type (
	SubscriberID string
	System       string
	Kind         string

	Event struct {
		System  System
		Kind    Kind
		Payload payload.Payload
	}
)

func NewKind(path ...string) Kind {
	return Kind(strings.Join(path, "."))
}
