package supervisor

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	processApi "github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/api/topology"
	"time"
)

// Service represents a running process service instance
type Service struct {
	id            registry.ID
	pid           pubsub.PID
	supervisorPID pubsub.PID
	config        processApi.ServiceConfig
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
	proc := processApi.GetProcesses(ctx)
	if proc == nil {
		return nil, fmt.Errorf("no process manager found in context")
	}

	// Setup monitor PID
	svc.supervisorPID = pubsub.PID{
		Node:   node.ID(),
		Host:   topology.ControlHost,
		UniqID: fmt.Sprintf("supervisor.%d", time.Now().UnixNano()),
	}

	// Create monitoring channel
	monitorCh := make(chan *pubsub.Batch, 1)

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
	pid, err := proc.StartMonitored(ctx, svc.supervisorPID, &processApi.StartProcess{
		HostID:   svc.config.HostID,
		ID:       processID,
		Payloads: payloads,
	})
	if err != nil {
		detach()
		return nil, fmt.Errorf("failed to start process: %w", err)
	}

	svc.pid = pid

	// Monitor process status
	go func() {
		defer close(svc.status)
		defer detach()

		for {
			select {
			case <-ctx.Done():
				return
			case batch, ok := <-monitorCh:
				if !ok {
					svc.status <- supervisor.ErrExit
					return
				}

				for _, msg := range *batch {
					if msg.Topic == processApi.TopicEvents {
						for _, p := range msg.Payloads {
							if event, ok := p.Data().(topology.MonitorEvent); ok {
								if event.Result.Error != nil {
									svc.status <- fmt.Errorf("process failed: %w", event.Result.Error)
								} else {
									svc.status <- supervisor.ErrExit
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

	processes := processApi.GetProcesses(ctx)
	if processes == nil {
		return fmt.Errorf("no process manager found in context")
	}

	return processes.Terminate(ctx, svc.pid)
}
