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
		// Register registers a PID that can be monitored.
		// This should be called before any process can be monitored.
		Register(pid pubsub.PID) error

		// Wait attaches a caller to monitor a specific PID.
		// Returns error if PID is not registered or already being monitored by caller.
		Wait(caller, pid pubsub.PID) error

		// Release removes a caller's monitoring of a specific PID.
		Release(caller, pid pubsub.PID) error

		// Notify sends monitoring events to all watchers of a PID.
		Notify(pid pubsub.PID, result *runtime.Result)

		// Remove completely removes a PID and all its watchers.
		// This should be called when a process terminates.
		Remove(pid pubsub.PID)
	}

	Event struct {
		At   time.Time
		Kind Kind
	}

	MonitorEvent struct {
		Event
		PID    pubsub.PID
		Result *runtime.Result
	}

	CancelEvent struct {
		Event
		Deadline time.Time
	}
)
