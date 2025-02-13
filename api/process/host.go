package process

import (
	"github.com/ponyruntime/pony/api/events"
)

// Event system and kind constants for the workflow package
const (
	HostSystem   events.System = "hosts"
	RegisterHost events.Kind   = "hosts.register"
	DeleteHost   events.Kind   = "hosts.remove"
	AcceptHost   events.Kind   = "hosts.accept"
	RejectHost   events.Kind   = "hosts.reject"
)
