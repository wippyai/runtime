package config

import "github.com/ponyruntime/pony/api"

const (
	ConfigGroup api.Component = "cfx"

	// Begin triggered once per transaction.
	Begin api.EventType = "begin"

	// AckState triggered by component based on change request.
	AckState api.EventType = "ack"

	// Deny triggered by component based on change request.
	Deny api.EventType = "deny"

	// ApplyState triggered once per transaction.
	ApplyState api.EventType = "apply"

	// DiscardState triggered once per transaction.
	DiscardState api.EventType = "discard"

	// Done triggered once per affected component to confirm apply.
	Done api.EventType = "done"
)
