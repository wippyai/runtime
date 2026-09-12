// SPDX-License-Identifier: MPL-2.0
package system

import (
	"context"
	"errors"
	"sync"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	topologyapi "github.com/wippyai/runtime/api/topology"
)

// A real scheduled process with no application-level monitor implementation.
// Receiving monitor control in Step is a harness failure, not a fake success.
type bootMonitorActor struct {
	closed chan struct{}
	once   sync.Once
}

func (*bootMonitorActor) Init(context.Context, string, payload.Payloads) error { return nil }
func (p *bootMonitorActor) Step(events []processapi.Event, out *processapi.StepOutput) error {
	for _, event := range events {
		if event.Type != processapi.EventMessage {
			continue
		}
		pkg, ok := event.Data.(*relay.Package)
		if !ok {
			return errors.New("unexpected native actor input")
		}
		valid := len(pkg.Messages) == 1 && pkg.Messages[0].Topic == "fixture:finish"
		relay.ReleasePackage(pkg)
		if !valid {
			return errors.New("mesh monitor control leaked into actor")
		}
		out.Done(nil)
		return nil
	}
	out.Idle()
	return nil
}
func (p *bootMonitorActor) Close() { p.once.Do(func() { close(p.closed) }) }

type bootMonitorLifecycle struct{ topology topologyapi.Topology }

func (l bootMonitorLifecycle) OnStart(_ context.Context, p pid.PID, _ processapi.Process) error {
	return l.topology.Register(p)
}
func (l bootMonitorLifecycle) OnComplete(_ context.Context, p pid.PID, result *runtimeapi.Result) {
	l.topology.Complete(p, result)
}

type bootMonitorInbox chan *relay.Package

func (in bootMonitorInbox) BindLocal(target pid.PID) (relay.ContextSender, error) {
	return boundBootMonitorInbox{in: in, target: target}, nil
}

type boundBootMonitorInbox struct {
	in     bootMonitorInbox
	target pid.PID
}

func (b boundBootMonitorInbox) SendContext(ctx context.Context, p *relay.Package) error {
	if !p.Target.Equal(b.target) {
		return relay.ErrBindingTarget
	}
	return b.in.SendContext(ctx, p)
}

func (in bootMonitorInbox) Send(p *relay.Package) error {
	return in.SendContext(context.Background(), p)
}
func (in bootMonitorInbox) SendContext(ctx context.Context, p *relay.Package) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case in <- p:
		return nil
	default:
		return errors.New("native monitor fixture inbox full")
	}
}
