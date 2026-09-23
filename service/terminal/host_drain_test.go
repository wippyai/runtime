// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	terminalapi "github.com/wippyai/runtime/api/service/terminal"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/logs"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

// drainingProcess waits after CANCEL for one more relay delivery, as a
// terminal process does when it waits for its child's exit before returning.
type drainingProcess struct {
	cancelled chan struct{}
	once      sync.Once
}

func (p *drainingProcess) Init(context.Context, string, payload.Payloads) error { return nil }

func (p *drainingProcess) Step(events []process.Event, out *process.StepOutput) error {
	for _, event := range events {
		pkg, ok := event.Data.(*relay.Package)
		if event.Type != process.EventMessage || !ok {
			continue
		}
		for _, message := range pkg.Messages {
			switch message.Topic {
			case topology.TopicEvents:
				p.once.Do(func() { close(p.cancelled) })
			case "exit":
				relay.ReleasePackage(pkg)
				out.Done(nil)
				return nil
			}
		}
		relay.ReleasePackage(pkg)
	}
	out.Idle()
	return nil
}

func (p *drainingProcess) Close() {}

func TestHostStopDeliversToDrainingProcess(t *testing.T) {
	scheduler := actor.NewScheduler(&mockCommandRegistry{}, actor.WithWorkers(1))
	h := NewHost(registry.ID{NS: "test", Name: "host1"}, &terminalapi.HostConfig{}, scheduler,
		&mockFactory{}, logs.NewConfigurator(nil, zap.NewNop()), zap.NewNop())
	_, err := h.Start(context.Background())
	require.NoError(t, err)

	processID := pid.PID{Node: "test-node", Host: "test:host1", UniqID: "draining"}
	processID = processID.Precomputed()
	proc := &drainingProcess{cancelled: make(chan struct{})}
	_, err = scheduler.Submit(context.Background(), processID, proc, "", nil)
	require.NoError(t, err)

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- h.Stop(stopCtx) }()

	select {
	case <-proc.cancelled:
	case <-time.After(time.Second):
		t.Fatal("process did not receive CANCEL")
	}
	_, err = h.Start(context.Background())
	require.ErrorIs(t, err, ErrHostShuttingDown, "a draining terminal host must not restart")
	pkg := relay.NewPackage(pid.PID{}, processID, "exit", payload.New(0))
	require.NoError(t, h.Send(pkg), "a draining terminal process must keep receiving deliveries")

	select {
	case err := <-stopped:
		require.NoError(t, err)
		require.NoError(t, stopCtx.Err(), "drain ran into the stop deadline")
	case <-time.After(6 * time.Second):
		t.Fatal("terminal host drain did not complete")
	}

	late := relay.NewPackage(pid.PID{}, processID, "exit", payload.New(0))
	require.ErrorIs(t, h.Send(late), ErrHostShuttingDown)
	relay.ReleasePackage(late)
}
