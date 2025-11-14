package supervisor

import (
	"context"
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	supervisorapi "github.com/wippyai/runtime/api/service/supervisor"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/internal/uniqid"
)

var supID = uniqid.NewGenerator()

// Service represents a running process service instance
type Service struct {
	id            registry.ID
	pid           relay.PID
	supervisorPID relay.PID
	config        supervisorapi.ServiceConfig
	status        chan any
}

// Start implements supervisor.Service
func (svc *Service) Start(ctx context.Context) (<-chan any, error) {
	// Get node from context
	node := relay.GetNode(ctx)
	if node == nil {
		return nil, fmt.Errorf("no node found in context")
	}

	// Get process manager from context
	proc := process.GetManager(ctx)
	if proc == nil {
		return nil, fmt.Errorf("no process manager found in context")
	}

	// Setup monitor pid
	svc.supervisorPID = relay.PID{
		Node: node.ID(),
		Host: topology.ControlHost,

		UniqID: supID.Generate(),
	}.Precomputed()

	// Create monitoring channel
	monitorCh := make(chan *relay.Package, 1)

	detach, err := node.Attach(svc.supervisorPID, monitorCh)
	if err != nil {
		return nil, fmt.Errorf("failed to attach monitor: %w", err)
	}

	// Initialize status channel
	svc.status = make(chan any, 1)

	processID := svc.config.Process
	if processID.NS == "" {
		processID.NS = svc.id.NS
	}

	var payloads payload.Payloads
	for _, p := range svc.config.Input {
		payloads = append(payloads, payload.New(p))
	}

	// Launch monitored process
	pid, err := proc.Start(ctx, &process.Start{
		HostID: svc.config.HostID,
		Source: processID,
		Input:  payloads,
		Lifecycle: process.Lifecycle{
			Parent:  svc.supervisorPID,
			Monitor: true,
		},
	})
	if err != nil {
		detach()
		return nil, fmt.Errorf("failed to start process: %w", err)
	}

	svc.pid = pid

	// Topology process status
	go func() {
		defer close(svc.status)
		defer detach()

		for {
			select {
			case <-ctx.Done():
				return
			case batch, ok := <-monitorCh:
				if !ok {
					select {
					case svc.status <- supervisor.ErrExit:
					default:
					}
					return
				}

				for _, msg := range batch.Messages {
					if msg.Topic == topology.TopicEvents {
						for _, p := range msg.Payloads {
							// we always require to pass system events within go runtime, to verify legitimacy
							if event, ok := p.Data().(*topology.ExitEvent); ok {
								if event.Result.Error != nil {
									select {
									case svc.status <- fmt.Errorf("process failed: %w", event.Result.Error):
									default:
									}
								} else {
									select {
									case svc.status <- supervisor.ErrExit:
									default:
									}
								}
								return
							}
						}
					}
				}
			}
		}
	}()

	return svc.status, nil
}

// Stop implements supervisor.Service
func (svc *Service) Stop(ctx context.Context) error {
	if svc.pid.UniqID == "" {
		return nil // Not running
	}

	err := relay.GetNode(ctx).Send(topology.Cancel(
		svc.supervisorPID,
		svc.pid,
		time.Now().Add(svc.config.Lifecycle.StopTimeout),
	))
	// FIXME handle error
	//nolint:revive,staticcheck // ignore for now
	if err != nil {
		// ignoring for now
	}

	// wait for completion
	select {
	case <-svc.status:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
