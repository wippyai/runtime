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
	"github.com/ponyruntime/pony/internal/uniqid"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

// DefaultHostMeta is the metadata key used to identify the default host for a process
const DefaultHostMeta = "default_host"
const DefaultCancelTimeout = 5 * time.Second // Default timeout for process cancellation

// Listener implements registry.EntryListener for bridging processes to functions.
// It listens for process.* entries with default_host metadata and creates function
// handlers that start processes and return their results.
type Listener struct {
	log        *zap.Logger
	bus        event.Bus
	procs      process.Manager
	uniqID     *uniqid.Generator
	registered map[string]pubsub.HostID // Map of registered process IDs to their host IDs
}

// NewListener creates a new process function bridge listener
func NewListener(log *zap.Logger, bus event.Bus, procs process.Manager) *Listener {
	return &Listener{
		log:        log,
		bus:        bus,
		procs:      procs,
		uniqID:     uniqid.NewGenerator(),
		registered: make(map[string]pubsub.HostID),
	}
}

// processEntry handles a registry entry based on the event kind
func (l *Listener) processEntry(ctx context.Context, kind event.Kind, entry registry.Entry) {
	// Skip if not a process entry
	if !strings.HasPrefix(entry.Kind, "process.") {
		return
	}

	processIDStr := entry.ID.String()
	defaultHost := entry.Meta.StringValue(DefaultHostMeta)

	switch kind {
	case registry.Create:
		// Skip if no default host
		if defaultHost == "" {
			return
		}
		// Register function handler
		l.registerFunction(ctx, entry.ID, defaultHost)

	case registry.Update:
		// If entry previously had a host but no longer does, unregister it
		if defaultHost == "" {
			if _, exists := l.registered[processIDStr]; exists {
				l.unregisterFunction(ctx, entry.ID)
			}
			return
		}

		// Check if host changed - if so, update registration
		if existingHost, exists := l.registered[processIDStr]; exists && existingHost != defaultHost {
			l.unregisterFunction(ctx, entry.ID)
			l.registerFunction(ctx, entry.ID, defaultHost)
			return
		}

		// If not previously registered, register it now
		if _, exists := l.registered[processIDStr]; !exists {
			l.registerFunction(ctx, entry.ID, defaultHost)
		}

	case registry.Delete:
		// Always unregister on delete if we registered it
		if _, exists := l.registered[processIDStr]; exists {
			l.unregisterFunction(ctx, entry.ID)
		}
	}
}

// registerFunction registers a process function handler in the function system
func (l *Listener) registerFunction(ctx context.Context, id registry.ID, hostID pubsub.HostID) {
	handler := l.createProcessHandler(id, hostID)

	// Register the function handler
	l.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   id.String(),
		Data:   handler,
	})

	// Track registered function
	l.registered[id.String()] = hostID

	l.log.Info("registered process function handler",
		zap.String("id", id.String()),
		zap.String("host", hostID))
}

// unregisterFunction removes a process function handler from the function system
func (l *Listener) unregisterFunction(ctx context.Context, id registry.ID) {
	idStr := id.String()

	// Send deregistration event
	l.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Delete,
		Path:   idStr,
	})

	// Remove from tracking map
	delete(l.registered, idStr)

	l.log.Info("unregistered process function handler", zap.String("id", idStr))
}

// createProcessHandler creates a function handler that starts a process and returns its result
func (l *Listener) createProcessHandler(processID registry.ID, hostID pubsub.HostID) function.Func {
	return func(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
		// Create result channel
		resultCh := make(chan *runtime.Result, 1)

		// Get node from context
		node := pubsub.GetNode(ctx)
		if node == nil {
			close(resultCh)
			return resultCh, fmt.Errorf("no pubsub node found in context")
		}

		// Create caller PID
		callerPID := pubsub.PID{
			Node:   node.ID(),
			Host:   topology.ControlHost,
			UniqID: l.uniqID.Generate(),
		}.Precomputed()

		// Create monitor channel for process events
		monitorCh := make(chan *pubsub.Package, 1)

		// Attach to pubsub
		detach, err := node.Attach(callerPID, monitorCh)
		if err != nil {
			close(resultCh)
			return resultCh, fmt.Errorf("failed to attach to pubsub: %w", err)
		}

		// Start process
		pid, err := l.procs.Start(ctx, &process.Start{
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

		l.log.Debug("started process function",
			zap.String("process_id", processID.String()),
			zap.String("pid", pid.String()))

		// Monitor process exit
		go func() {
			defer close(resultCh)
			defer detach()

			for {
				select {
				case <-ctx.Done():
					// Context canceled - release the monitor first
					topo := topology.GetTopology(ctx)
					if topo != nil {
						if err := topo.Release(callerPID, pid); err != nil {
							l.log.Warn("failed to release monitor before cancel",
								zap.String("pid", pid.String()),
								zap.Error(err))
						}
					} else {
						l.log.Warn("topology not found in context, skipping monitor release")
					}

					// Send cancel request
					if err := node.Send(topology.Cancel(
						callerPID,
						pid,
						time.Now().Add(DefaultCancelTimeout),
					)); err != nil {
						l.log.Warn("failed to send cancel request",
							zap.String("pid", pid.String()),
							zap.Error(err))
					}

					// Return context error immediately
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
								if e, ok := p.Data().(*topology.ExitEvent); ok {
									// Forward process result to function result channel
									l.log.Debug("received exit event from process",
										zap.String("process_id", processID.String()),
										zap.String("pid", pid.String()),
										zap.Error(e.Result.Error))

									resultCh <- e.Result
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

// WithProcessFunctionBridge creates an event handler that bridges processes to functions
func WithProcessFunctionBridge(log *zap.Logger, bus event.Bus, procs process.Manager) eventbus.EventHandler {
	listener := NewListener(log, bus, procs)

	// Create base handler that subscribes to registry events
	return eventbus.NewBaseHandler(
		eventbus.Pattern{
			System: registry.System,
			Kind:   registry.Changes,
		},
		func(ctx context.Context, evt event.Event) error {
			entry, ok := evt.Data.(registry.Entry)
			if !ok {
				// Not a registry entry event
				return nil
			}

			// Process the entry based on event kind
			listener.processEntry(ctx, evt.Kind, entry)
			return nil
		},
	)
}
