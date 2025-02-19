package topology

import (
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"time"
)

const (
	ControlHost pubsub.HostID = "node:control"
	TopicEvents pubsub.Topic  = "@pid/events"
	KindCancel  Kind          = "pid.cancel"
	KindMonitor Kind          = "pid.result"
)

type (
	Kind = string

	Monitor interface {
		// Register registers a pid that can be monitored.
		// This should be called before any process can be monitored.
		Register(pid pubsub.PID) error

		// Wait attaches a caller to monitor a specific pid.
		// Returns error if pid is not registered or already being monitored by caller.
		Wait(caller, pid pubsub.PID) error

		// Release removes a caller's monitoring of a specific pid.
		Release(caller, pid pubsub.PID) error

		// Notify sends monitoring events to all watchers of a pid.
		Notify(pid pubsub.PID, result *runtime.Result)

		// Remove completely removes a pid and all its watchers.
		// This should be called when a process terminates.
		Remove(pid pubsub.PID)
	}

	Event struct {
		At   time.Time `json:"at"`
		Kind Kind      `json:"kind"`
	}

	MonitorEvent struct {
		Event  Event           `json:"event"`
		PID    pubsub.PID      `json:"pid"`
		Result *runtime.Result `json:"result"`
	}

	CancelEvent struct {
		Event    Event     `json:"event"`
		Deadline time.Time `json:"deadline"`
	}
)
