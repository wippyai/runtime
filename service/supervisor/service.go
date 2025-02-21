package supervisor

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	supervisor2 "github.com/ponyruntime/pony/api/service/supervisor"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/internal/uniqid"

	"time"
)

var supID = uniqid.NewGenerator()

// Service represents a running process service instance
type Service struct {
	id            registry.ID
	pid           pubsub.PID
	supervisorPID pubsub.PID
	config        supervisor2.ServiceConfig
	status        chan any
}

// Start implements supervisor.Service
func (svc *Service) Start(ctx context.Context) (<-chan any, error) {
	// Get node from context
	node := pubsub.GetNode(ctx)
	if node == nil {
		return nil, fmt.Errorf("no node found in context")
	}

	// Get process manager from context
	proc := process.GetProcessManager(ctx)
	if proc == nil {
		return nil, fmt.Errorf("no process manager found in context")
	}

	// Setup monitor pid
	svc.supervisorPID = pubsub.PID{
		Node:   node.ID(),
		Host:   topology.ControlHost,
		ID:     registry.ID{Name: "supervisor"},
		UniqID: supID.Generate(),
	}

	// Create monitoring channel
	monitorCh := make(chan *pubsub.Package, 1)

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
	pid, err := proc.StartMonitored(ctx, svc.supervisorPID, &process.StartProcess{
		HostID:   svc.config.HostID,
		ID:       processID,
		Payloads: payloads,
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
					if msg.Topic == process.TopicEvents {
						for _, p := range msg.Payloads {
							// we always require to pass system events within go runtime, to verify legitimacy
							if event, ok := p.Data().(topology.ResultEvent); ok {
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
	if svc.pid.ID.Name == "" {
		return nil // Not running
	}

	err := pubsub.GetNode(ctx).Send(
		ctx,
		topology.Cancel(svc.supervisorPID, svc.pid, time.Now().Add(svc.config.Lifecycle.StopTimeout)),
	)
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
