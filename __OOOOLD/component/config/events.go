package config

import (
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
)

type Event struct {
	Component events.System
	Type      events.Kind
	payload.Payload
}

const (
	ConfigGroup events.System = "cfx"

	// Begin triggered once per transaction.
	Begin events.Kind = "begin"

	// AckState triggered by components based on change request.
	AckState events.Kind = "ack"

	// Deny triggered by components based on change request.
	Deny events.Kind = "deny"

	// Apply triggered once per transaction.
	Apply events.Kind = "apply"

	// Discard triggered once per transaction.
	Discard events.Kind = "discard"

	// Done triggered once per affected components to confirm apply.
	Done events.Kind = "done"
)
