package config

import "github.com/ponyruntime/pony/api"

const (
	Group api.Component = "cfx"

	// Begin triggered once per transaction.
	Begin api.EventType = "begin"

	// Ack triggered by component based on change request.
	Ack api.EventType = "ack"

	// Deny triggered by component based on change request.
	Deny api.EventType = "deny"

	// Apply triggered once per transaction.
	Apply api.EventType = "apply"

	// Discard triggered once per transaction.
	Discard api.EventType = "discard"

	// Done triggered once per affected component to confirm apply.
	Done api.EventType = "done"
)
