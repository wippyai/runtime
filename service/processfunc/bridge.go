package processfunc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/topology"
	"go.uber.org/zap"
)

// Manager listens for registry entries with process.* kinds and default_host metadata,
// creating function handlers that bridge the function and process systems.
type Manager struct {
	log   *zap.Logger
	bus   event.Bus
	procs process.Manager
}

// NewManager creates a new process function bridge manager
func NewManager(log *zap.Logger, bus event.Bus, procs process.Manager) *Manager {
	return &Manager{
		log:   log,
		bus:   bus,
		procs: procs,
	}
}

// Add implements registry.EntryListener
// Creates a function handler for process.* entries with default_host metadata
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	// Check if entry kind starts with "process."
	if !strings.HasPrefix(entry.Kind, "process.") {
		return nil // Not a process entry
	}

	// Check if it has default_host metadata
	defaultHost := entry.Meta.StringValue("default_host")
	if defaultHost == "" {
		return nil // No default host specified
	}

	// Register function handler
	m.registerHandler(ctx, entry.ID, defaultHost)

	m.log.Info("registered process function handler",
		zap.String("id", entry.ID.String()),
		zap.String("host", defaultHost))

	return nil
}

// Update implements registry.EntryListener
// Updates function handler when process entries change
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	// Check if entry kind starts with "process."
	if !strings.HasPrefix(entry.Kind, "process.") {
		return nil // Not a process entry
	}

	// Unregister existing handler first
	m.unregisterHandler(ctx, entry.ID)

	// Check if it has default_host metadata in updated entry
	defaultHost := entry.Meta.StringValue("default_host")
	if defaultHost == "" {
		return nil // No default host specified
	}

	// Register new handler with updated config
	m.registerHandler(ctx, entry.ID, defaultHost)

	m.log.Info("updated process function handler",
		zap.String("id", entry.ID.String()),
		zap.String("host", defaultHost))

	return nil
}

// Delete implements registry.EntryListener
// Removes function handler when process entries are deleted
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	// Check if entry kind starts with "process."
	if !strings.HasPrefix(entry.Kind, "process.") {
		return nil // Not a process entry
	}

	// Unregister function handler
	m.unregisterHandler(ctx, entry.ID)

	m.log.Info("unregistered process function handler",
		zap.String("id", entry.ID.String()))

	return nil
}

// registerHandler registers a process function handler in the function system
func (m *Manager) registerHandler(ctx context.Context, id registry.ID, hostID pubsub.HostID) {
	handler := m.createHandler(id, hostID)

	m.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   id.String(),
		Data:   handler,
	})
}

// unregisterHandler removes a process function handler from the function system
func (m *Manager) unregisterHandler(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Delete,
		Path:   id.String(),
	})
}

// createHandler creates a function handler that starts a process and returns its result
func (m *Manager) createHandler(processID registry.ID, hostID pubsub.HostID) function.Func {
	return func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		// Create result channel
		resultCh := make(chan *runtime.Result, 1)

		// Get node from context
		node := pubsub.GetNode(ctx)
		if node == nil {
			close(resultCh)
			return resultCh, fmt.Errorf("no pubsub node found in context")
		}

		// Generate unique ID for process caller
		callerUniqID := fmt.Sprintf("processfunc-%s", task.ID.String())

		// Create caller PID
		callerPID := pubsub.PID{
			Node:   node.ID(),
			Host:   topology.ControlHost,
			ID:     registry.ID{Name: "processfunc"},
			UniqID: callerUniqID,
		}.WithCachedString()

		// Create monitor channel for process events
		monitorCh := make(chan *pubsub.Package, 1)

		// Attach to pubsub
		detach, err := node.Attach(callerPID, monitorCh)
		if err != nil {
			close(resultCh)
			return resultCh, fmt.Errorf("failed to attach to pubsub: %w", err)
		}

		// Start process
		pid, err := m.procs.Start(ctx, &process.Start{
			HostID: hostID,
			Source: processID,
			Input:  task.Payloads,
			Lifecycle: process.Lifecycle{
				Parent:  callerPID,
				Monitor: true,
			},
		})

		if err != nil {
			detach()
			close(resultCh)
			return resultCh, fmt.Errorf("failed to start process: %w", err)
		}

		m.log.Debug("started process function",
			zap.String("process_id", processID.String()),
			zap.String("pid", pid.String()))

		// Monitor process exit
		go func() {
			defer close(resultCh)
			defer detach()

			for {
				select {
				case <-ctx.Done():
					// Context canceled, attempt to gracefully cancel the process
					err := node.Send(topology.Cancel(
						callerPID,
						pid,
						time.Now().Add(5*time.Second), // Give process 5 seconds to shutdown gracefully
					))
					if err != nil {
						m.log.Error("failed to send cancel request", zap.Error(err))
					}

					// Send context error
					resultCh <- &runtime.Result{
						Error: ctx.Err(),
					}
					return

				case batch, ok := <-monitorCh:
					if !ok {
						// Channel closed unexpectedly
						resultCh <- &runtime.Result{
							Error: fmt.Errorf("monitor channel closed unexpectedly"),
						}
						return
					}

					for _, msg := range batch.Messages {
						if msg.Topic == topology.TopicEvents {
							for _, p := range msg.Payloads {
								// Process exit event
								if event, ok := p.Data().(*topology.ExitEvent); ok {
									// Forward process result to function result channel
									resultCh <- event.Result
									return
								}
							}
						}
					}
				}
			}
		}()

		return resultCh, nil
	}
}
