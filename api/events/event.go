package events

import (
	"strings"
)

type (
	SubscriberID string
	System       string
	Kind         string

	Event struct {
		System System
		Kind   Kind
		Data   any
	}
)

func NewKind(path ...string) Kind {
	return Kind(strings.Join(path, "."))
}
