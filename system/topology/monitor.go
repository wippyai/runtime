package topology

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/topology"
	"sync"
	"time"
)

type monitor struct {
	ctx      context.Context
	monitors sync.Map
	upstream pubsub.Upstream
}

func NewMonitor(ctx context.Context, upstream pubsub.Upstream) topology.Monitor {
	return &monitor{
		ctx:      ctx,
		upstream: upstream,
	}
}

func (m *monitor) Wait(caller, pid pubsub.PID) error {
	value, _ := m.monitors.LoadOrStore(pid.String(), &sync.Map{})
	watchers := value.(*sync.Map)

	_, loaded := watchers.LoadOrStore(caller.String(), true)
	if loaded {
		return fmt.Errorf("already monitoring pid: %s", pid.String())
	}

	return nil
}

func (m *monitor) Release(caller, pid pubsub.PID) error {
	value, ok := m.monitors.Load(pid.String())
	if !ok {
		return nil
	}
	watchers := value.(*sync.Map)

	watchers.Delete(caller.String())

	empty := true
	watchers.Range(func(key, value interface{}) bool {
		empty = false
		return false
	})
	if empty {
		m.monitors.Delete(pid.String())
	}

	return nil
}

func (m *monitor) Notify(pid pubsub.PID, result *runtime.Result) {
	value, ok := m.monitors.Load(pid.String())
	if !ok {
		return
	}
	watchers := value.(*sync.Map)

	watchers.Range(func(key, _ interface{}) bool {
		watcherPID, err := pubsub.ParsePID(key.(string))
		if err != nil {
			return true
		}

		batch := pubsub.NewBatch(
			process.TopicEvents,
			payload.New(topology.MonitorEvent{
				Event:  topology.Event{At: time.Now(), Kind: topology.TopicCancel},
				PID:    pid,
				Result: result,
			}),
		)

		err = m.upstream.Send(m.ctx, watcherPID, batch)
		if err != nil {
			return true
		}
		return true
	})
}

func (m *monitor) Remove(pid pubsub.PID) {
	m.monitors.Delete(pid.String())
}
