// Package processfunc provides a bridge between process entries and function handlers.
package processfunc

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	topapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/internal/uniqid"
	"go.uber.org/zap"
)

const DefaultCancelTimeout = 5 * time.Second

// Listener bridges process.* registry entries to function handlers.
// When a process entry has default_host in options/meta, it registers
// a function handler that starts the process and returns its result.
type Listener struct {
	log     *zap.Logger
	bus     event.Bus
	pidGen  *uniqid.PIDGenerator
	node    relay.Node
	topo    topapi.Topology
	manager process.Manager

	mu         sync.RWMutex
	registered map[string]pid.HostID
}

// NewListener creates a new process function bridge listener.
func NewListener(
	log *zap.Logger,
	bus event.Bus,
	pidGen *uniqid.PIDGenerator,
	node relay.Node,
	topo topapi.Topology,
	manager process.Manager,
) *Listener {
	return &Listener{
		log:        log,
		bus:        bus,
		pidGen:     pidGen,
		node:       node,
		topo:       topo,
		manager:    manager,
		registered: make(map[string]pid.HostID),
	}
}

// Add implements registry.EntryListener.
func (l *Listener) Add(ctx context.Context, entry registry.Entry) error {
	l.processEntry(ctx, registry.Create, entry)
	return nil
}

// Update implements registry.EntryListener.
func (l *Listener) Update(ctx context.Context, entry registry.Entry) error {
	l.processEntry(ctx, registry.Update, entry)
	return nil
}

// Delete implements registry.EntryListener.
func (l *Listener) Delete(ctx context.Context, entry registry.Entry) error {
	l.processEntry(ctx, registry.Delete, entry)
	return nil
}

// processEntry handles a registry entry based on the event kind.
func (l *Listener) processEntry(ctx context.Context, kind event.Kind, entry registry.Entry) {
	if !strings.HasPrefix(entry.Kind, "process.") {
		return
	}

	// Extract options bag if present
	opts, hasOptions := entry.Meta.GetBag("options")

	// Get default_host from options first, fallback to meta
	defaultHostStr := ""
	if hasOptions {
		defaultHostStr = opts.GetString("default_host", "")
	}

	if defaultHostStr == "" {
		defaultHostStr = entry.Meta.GetString("default_host", "")
		if defaultHostStr != "" {
			// Ensure options bag exists and has default_host
			if !hasOptions {
				opts = attrs.NewBag()
			}
			opts.Set("default_host", defaultHostStr)
		}
	}

	// No default_host found anywhere - skip or unregister
	if defaultHostStr == "" {
		opts = nil
	}

	defaultHost := defaultHostStr
	idStr := entry.ID.String()

	// No default_host - unregister if previously registered
	if defaultHost == "" {
		l.mu.RLock()
		_, exists := l.registered[idStr]
		l.mu.RUnlock()
		if exists {
			l.unregisterFunction(ctx, idStr)
		}
		return
	}

	switch kind {
	case registry.Create:
		l.registerFunction(ctx, idStr, defaultHost, opts)

	case registry.Update:
		l.mu.RLock()
		existingHost, exists := l.registered[idStr]
		l.mu.RUnlock()

		if exists && existingHost != defaultHost {
			l.unregisterFunction(ctx, idStr)
			l.registerFunction(ctx, idStr, defaultHost, opts)
			return
		}

		if !exists {
			l.registerFunction(ctx, idStr, defaultHost, opts)
		}

	case registry.Delete:
		l.mu.RLock()
		_, exists := l.registered[idStr]
		l.mu.RUnlock()
		if exists {
			l.unregisterFunction(ctx, idStr)
		}
	}
}

// registerFunction registers a process function handler.
func (l *Listener) registerFunction(ctx context.Context, idStr string, hostID pid.HostID, opts attrs.Bag) {
	handler := &processHandler{
		log:       l.log,
		pidGen:    l.pidGen,
		node:      l.node,
		topo:      l.topo,
		manager:   l.manager,
		processID: idStr,
		hostID:    hostID,
	}

	l.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   idStr,
		Data: &function.FuncEntry{
			Handler: handler.Call,
			Options: opts,
		},
	})

	l.mu.Lock()
	l.registered[idStr] = hostID
	l.mu.Unlock()

	l.log.Debug("registered process function handler",
		zap.String("id", idStr),
		zap.String("host", hostID))
}

// unregisterFunction removes a process function handler.
func (l *Listener) unregisterFunction(ctx context.Context, idStr string) {
	l.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Delete,
		Path:   idStr,
	})

	l.mu.Lock()
	delete(l.registered, idStr)
	l.mu.Unlock()

	l.log.Debug("unregistered process function handler", zap.String("id", idStr))
}

// processHandler handles function calls by starting a process and returning its result.
type processHandler struct {
	log       *zap.Logger
	pidGen    *uniqid.PIDGenerator
	node      relay.Node
	topo      topapi.Topology
	manager   process.Manager
	processID string
	hostID    pid.HostID
}

// Call implements function.Func via TOCTOU-safe monitoring.
func (h *processHandler) Call(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	// Generate caller PID for monitoring (using control host)
	callerPID := h.pidGen.Generate(topapi.ControlHost)

	// Register caller in topology FIRST
	if err := h.topo.Register(callerPID); err != nil {
		return nil, newRegisterPIDError(err)
	}
	defer h.topo.Remove(callerPID)

	// Attach to relay to receive exit events
	monitorCh := make(chan *relay.Package, 1)
	detach, err := h.node.Attach(callerPID, monitorCh)
	if err != nil {
		return nil, newAttachRelayError(err)
	}
	defer detach()

	// Prepare start options with monitoring
	options := attrs.NewBag()
	options.Set(process.LifecycleParentKey, callerPID)
	options.Set(process.LifecycleMonitorKey, true)

	// Start process - lifecycle.OnStart will atomically:
	// - Register child PID in topology
	// - Call topology.Wait(callerPID, childPID)
	processPID, err := h.manager.Start(ctx, &process.Start{
		HostID:  h.hostID,
		Source:  registry.ParseID(h.processID),
		Input:   task.Payloads,
		Options: options,
	})
	if err != nil {
		return nil, newStartProcessError(err)
	}

	pidStr := processPID.String()
	h.log.Debug("started process function",
		zap.String("process_id", h.processID),
		zap.String("pid", pidStr))

	// Monitor for exit (blocking)
	for {
		select {
		case <-ctx.Done():
			// Context canceled - release monitor and send cancel
			_ = h.topo.Demonitor(callerPID, processPID)

			if err := h.node.Send(topapi.CancelPackage(
				callerPID,
				processPID,
				time.Now().Add(DefaultCancelTimeout),
			)); err != nil {
				h.log.Warn("failed to send cancel",
					zap.String("pid", pidStr),
					zap.Error(err))
			}

			return &runtime.Result{Error: ctx.Err()}, nil

		case batch, ok := <-monitorCh:
			if !ok {
				return &runtime.Result{
					Error: ErrMonitorChannelClosed,
				}, nil
			}

			for _, msg := range batch.Messages {
				if msg.Topic != topapi.TopicEvents {
					continue
				}
				for _, p := range msg.Payloads {
					if e, ok := p.Data().(*topapi.ExitEvent); ok {
						h.log.Debug("received exit event",
							zap.String("process_id", h.processID),
							zap.String("pid", pidStr))
						return e.Result, nil
					}
				}
			}
		}
	}
}

var _ registry.EntryListener = (*Listener)(nil)
