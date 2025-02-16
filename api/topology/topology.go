package topology

import (
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"time"
)

const (
	TopicEvents pubsub.Topic = "@pid/events"
	TopicCancel pubsub.Topic = "@pid/cancel"

	KindCancel  Kind = "pid.cancel"
	KindMonitor Kind = "pid.result"
)

type (
	Kind = string

	Monitor interface {
		Wait(caller, pid pubsub.PID) error
		Release(caller, pid pubsub.PID) error

		Notify(pid pubsub.PID, result *runtime.Result)
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
	}
)
