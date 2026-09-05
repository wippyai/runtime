// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"errors"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	"github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	sysprocess "github.com/wippyai/runtime/system/process"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

// ActorProcess owns one guest execution for its entire PID lifetime. Function
// pools continue using Process, whose warm-instance reset contract is different.
type ActorProcess struct {
	*Process
	mailbox          *actor.Mailbox
	releaseResources func()
	socketBudget     *preview2.SocketBudget
	mailboxLimits    actor.Limits
	initialized      bool
}

func NewActorProcess(execution *Process, limits actor.Limits, releaseResources func()) *ActorProcess {
	return &ActorProcess{Process: execution, mailboxLimits: limits, releaseResources: releaseResources}
}

// SetSocketBudget configures the shared socket budget before Init.
func (p *ActorProcess) SetSocketBudget(budget *preview2.SocketBudget) {
	if p.initialized {
		panic("actor socket budget cannot change after Init")
	}
	p.socketBudget = budget
}

// SocketBudget returns the shared socket budget for this actor execution, if set.
func (p *ActorProcess) SocketBudget() *preview2.SocketBudget {
	return p.socketBudget
}

func (p *ActorProcess) Init(ctx context.Context, method string, input payload.Payloads) error {
	if p.initialized {
		return errors.New("WASM actor already initialized")
	}
	if p.Process == nil || p.module == nil {
		return errors.New("WASM actor module required")
	}
	p.initialized = true
	p.mailbox = actor.NewMailbox(p.mailboxLimits)
	if p.socketBudget != nil {
		ctx = wippyhost.WithSocketBudget(ctx, p.socketBudget)
	}
	return p.Process.Init(actor.WithMailbox(ctx, p.mailbox), method, input)
}

func (p *ActorProcess) EventAdmission() process.EventAdmission { return p.mailbox }

func (p *ActorProcess) ExecutionTimeout() time.Duration {
	if p.Process == nil {
		return 0
	}
	return time.Duration(p.limits.MaxExecutionMS) * time.Millisecond
}

func (p *ActorProcess) Step(events []process.Event, out *process.StepOutput) error {
	var firstErr error
	for _, event := range events {
		if event.Type != process.EventMessage {
			continue
		}
		if p.mailbox.Deliver(event) {
			continue
		}
		pkg, ok := event.Data.(*relay.Package)
		if !ok || pkg == nil {
			continue
		}
		if pkg.Source == topology.SystemPID {
			for _, msg := range pkg.Messages {
				if msg == nil || msg.Topic != topology.TopicEvents {
					continue
				}
				for _, pl := range msg.Payloads {
					if pl != nil {
						if _, ok := pl.Data().(*topology.CancelEvent); ok {
							firstErr = sysprocess.ErrTerminated
						}
					}
				}
			}
			relay.ReleasePackage(pkg)
			continue
		}
		// Direct Step callers and scheduler-internal sends still obey admission.
		admitted, err := p.mailbox.AdmitEvent(event)
		if err != nil {
			relay.ReleasePackage(pkg)
			if firstErr == nil {
				firstErr = err
			}
		} else {
			p.mailbox.Deliver(admitted)
		}
	}
	if firstErr != nil {
		return firstErr
	}
	return p.Process.Step(events, out)
}

func (p *ActorProcess) Close() {
	p.Process.Close()
	if p.mailbox != nil {
		p.mailbox.Close()
	}
	if p.releaseResources != nil {
		p.releaseResources()
		p.releaseResources = nil
	}
}
