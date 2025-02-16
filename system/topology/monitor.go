package topology

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/topology"
	"log"
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
	value, _ := m.monitors.LoadOrStore(pid, &sync.Map{})
	watchers := value.(*sync.Map)

	_, loaded := watchers.LoadOrStore(caller, true)
	if loaded {
		return fmt.Errorf("already monitoring pid: %s", pid)
	}

	return nil
}

func (m *monitor) Release(caller, pid pubsub.PID) error {
	value, ok := m.monitors.Load(pid)
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

	value, ok := m.monitors.Load(pid)
	if !ok {
		return
	}
	watchers := value.(*sync.Map)
	watchers.Range(func(key, _ interface{}) bool {
		watcherPID, ok := key.(pubsub.PID)
		if !ok {
			return true
		}

		batch := pubsub.NewBatch(
			process.TopicEvents,
			payload.New(topology.MonitorEvent{
				Event:  topology.Event{At: time.Now(), Kind: topology.KindMonitor},
				Result: result,
			}),
		)

		if err := m.upstream.Send(m.ctx, watcherPID, batch); err != nil {
			return true
		}
		return true
	})

}

func (m *monitor) Remove(pid pubsub.PID) {
	go func() {
		time.Sleep(1 * time.Second)
		log.Printf("REMOVE: %s", pid)
	}()
	m.monitors.Delete(pid.String())
}
