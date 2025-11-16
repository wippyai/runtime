package processfunc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

const DefaultCancelTimeout = 5 * time.Second // Default timeout for process cancellation

// Listener implements registry.EntryListener for bridging processes to functions.
// It listens for process.* entries with options containing default_host and creates
// function handlers that start processes and return their results.
type Listener struct {
	log        *zap.Logger
	bus        event.Bus
	procs      process.Manager
	uniqID     *uniqid.Generator
	registered map[string]relay.HostID // Map of registered process IDs to their host IDs
}

// NewListener creates a new process function bridge listener
func NewListener(log *zap.Logger, bus event.Bus, procs process.Manager) *Listener {
	return &Listener{
		log:        log,
		bus:        bus,
		procs:      procs,
		uniqID:     uniqid.NewGenerator(),
		registered: make(map[string]relay.HostID),
	}
}

// processEntry handles a registry entry based on the event kind
func (l *Listener) processEntry(ctx context.Context, kind event.Kind, entry registry.Entry) {
	// Skip if not a process entry
	if !strings.HasPrefix(entry.Kind, "process.") {
		return
	}

	processIDStr := entry.ID.String()

	// Extract options bag (new notation) or create one for normalization (old notation)
	var opts registry.Metadata

	if optsBag, hasOptions := entry.Meta.GetBag("options"); hasOptions {
		opts = optsBag
	} else {
		// Old notation: create options bag for normalization
		opts = attrs.NewBag()
	}

	// Get default_host from options (new notation) or fallback to Meta (old notation)
	defaultHost := opts.GetString("default_host", "")
	if defaultHost == "" {
		defaultHost = entry.Meta.GetString("default_host", "")
		if defaultHost != "" {
			// Normalize: put default_host into options for processfunc system
			opts.Set("default_host", defaultHost)
		}
	}

	// If opts is still empty, set to nil
	if len(opts) == 0 {
		opts = nil
	}

	// If no default_host found anywhere, unregister if previously registered
	if defaultHost == "" {
		if _, exists := l.registered[processIDStr]; exists {
			l.unregisterFunction(ctx, entry.ID)
		}
		return
	}

	switch kind {
	case registry.Create:
		l.registerFunction(ctx, entry.ID, defaultHost, opts)

	case registry.Update:
		// Check if host changed - if so, update registration
		if existingHost, exists := l.registered[processIDStr]; exists && existingHost != defaultHost {
			l.unregisterFunction(ctx, entry.ID)
			l.registerFunction(ctx, entry.ID, defaultHost, opts)
			return
		}

		// If not previously registered, register it now
		if _, exists := l.registered[processIDStr]; !exists {
			l.registerFunction(ctx, entry.ID, defaultHost, opts)
		}

	case registry.Delete:
		// Always unregister on delete if we registered it
		if _, exists := l.registered[processIDStr]; exists {
			l.unregisterFunction(ctx, entry.ID)
		}
	}
}

// registerFunction registers a process function handler in the function system
func (l *Listener) registerFunction(ctx context.Context, id registry.ID, hostID relay.HostID, opts registry.Metadata) {
	handler := l.createProcessHandler(id, hostID)

	// Register the function handler with options
	l.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   id.String(),
		Data: &function.FuncEntry{
			Handler: handler,
			Options: opts,
		},
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
func (l *Listener) createProcessHandler(processID registry.ID, hostID relay.HostID) function.Func {
	return func(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
		// Get node from context
		node := relay.GetNode(ctx)
		if node == nil {
			return nil, fmt.Errorf("no relay node found in context")
		}

		// Create caller PID
		callerPID := relay.PID{
			Node:   node.ID(),
			Host:   topology.ControlHost,
			UniqID: l.uniqID.Generate(),
		}.Precomputed()

		// Create monitor channel for process events
		monitorCh := make(chan *relay.Package, 1)

		// Attach to relay
		detach, err := node.Attach(callerPID, monitorCh)
		if err != nil {
			return nil, fmt.Errorf("failed to attach to relay: %w", err)
		}
		defer detach()

		// Start process
		options := attrs.NewBag()
		options.Set(process.LifecycleParentKey, callerPID)
		options.Set(process.LifecycleMonitorKey, true)

		pid, err := l.procs.Start(ctx, &process.Start{
			HostID:  hostID,
			Source:  processID,
			Input:   task.Payloads,
			Options: options,
		})

		if err != nil {
			return nil, fmt.Errorf("failed to start process: %w", err)
		}

		l.log.Debug("started process function",
			zap.String("process_id", processID.String()),
			zap.String("pid", pid.String()))

		// Monitor process exit (blocking)
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
				return &runtime.Result{
					Error: ctx.Err(),
				}, nil

			case batch, ok := <-monitorCh:
				if !ok {
					// Channel closed unexpectedly
					return &runtime.Result{
						Error: fmt.Errorf("monitor channel closed unexpectedly"),
					}, nil
				}

				for _, msg := range batch.Messages {
					if msg.Topic == topology.TopicEvents {
						for _, p := range msg.Payloads {
							// Process exit event
							if e, ok := p.Data().(*topology.ExitEvent); ok {
								// Return process result
								l.log.Debug("received exit event from process",
									zap.String("process_id", processID.String()),
									zap.String("pid", pid.String()),
									zap.Error(e.Result.Error))

								return e.Result, nil
							}
						}
					}
				}
			}
		}
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
