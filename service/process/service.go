package process

import (
	"context"
	"fmt"
	processApi "github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/topology"
	"sync"
	"time"
)

// Service represents a running process service instance
type Service struct {
	mu     sync.Mutex
	pid    pubsub.PID
	id     registry.ID
	config processApi.ServiceConfig
	status chan any
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
	monitorPID := pubsub.PID{
		Node:   node.ID(),
		Host:   topology.ControlHost,
		UniqID: fmt.Sprintf("monitor-%d", time.Now().UnixNano()),
	}

	// Create monitoring channel
	monitorCh := make(chan *pubsub.Batch, 10)

	// Attach monitor to control host
	controlHost := node.GetHost(topology.ControlHost)
	if controlHost == nil {
		return nil, fmt.Errorf("control host not found")
	}

	err, detach := controlHost.Attach(monitorPID, monitorCh)
	if err != nil {
		return nil, fmt.Errorf("failed to attach monitor: %w", err)
	}

	// Initialize status channel
	svc.status = make(chan any, 1)

	// Launch monitored process
	pid, err := proc.StartMonitored(ctx, monitorPID, processApi.StartProcess{
		HostID: svc.config.HostID,
		ID:     svc.config.ID,
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
			case batch := <-monitorCh:
				for _, msg := range *batch {
					if msg.Topic == processApi.TopicEvents {
						for _, p := range msg.Payloads {
							if event, ok := p.Data().(topology.MonitorEvent); ok {
								if event.Result.Error != nil {
									svc.status <- fmt.Errorf("process failed: %w", event.Result.Error)
								} else {
									svc.status <- event.Result.Payload
								}
								return // Exit after receiving result
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
	svc.mu.Lock()
	defer svc.mu.Unlock()

	if svc.pid.ID.Name == "" {
		return nil // Not running
	}

	processes := processApi.GetProcesses(ctx)
	if processes == nil {
		return fmt.Errorf("no process manager found in context")
	}

	return processes.Terminate(ctx, svc.pid)
}
