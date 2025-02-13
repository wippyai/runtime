package processes

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
	"sync"
)

type hostInfo struct {
	host      process.Host
	delegated bool
}

// HostRegistry manages process hosts and their lifecycle
type HostRegistry struct {
	ctx        context.Context
	log        *zap.Logger
	bus        events.Bus
	hosts      sync.Map // map[string]hostInfo
	subscriber *eventbus.Subscriber
}

// NewHostRegistry creates a new host registry instance
func NewHostRegistry(bus events.Bus, logger *zap.Logger) *HostRegistry {
	return &HostRegistry{
		log: logger,
		bus: bus,
	}
}

// Start begins listening for host registration events
func (r *HostRegistry) Start(ctx context.Context) error {
	r.ctx = ctx

	sub, err := eventbus.NewSubscriber(
		r.ctx,
		r.bus,
		process.HostSystem,
		"hosts.(register|remove)",
		r.handleEvent,
	)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	r.subscriber = sub

	return nil
}

// Stop cleans up registry resources
func (r *HostRegistry) Stop() error {
	if r.subscriber != nil {
		r.subscriber.Close()
	}
	return nil
}

func (r *HostRegistry) handleEvent(e events.Event) {
	switch e.Kind {
	case process.RegisterHost:
		r.registerHost(e)
	case process.DeleteHost:
		r.deleteHost(e)
	default:
		r.log.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (r *HostRegistry) registerHost(e events.Event) {
	host, ok := e.Data.(process.Host)
	if !ok {
		r.log.Error("invalid host payload",
			zap.String("host", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		r.sendReject(e.Path, "invalid host data type")
		return
	}

	// Determine host type
	delegated := false
	switch h := host.(type) {
	case process.Managed:
		_ = h // avoid unused variable warning
	case process.Delegated:
		delegated = true
		_ = h // avoid unused variable warning
	default:
		r.log.Error("invalid host implementation",
			zap.String("host", e.Path),
			zap.String("type", fmt.Sprintf("%T", host)))
		r.sendReject(e.Path, "host must implement either Managed or Delegated interface")
		return
	}

	info := hostInfo{host: host, delegated: delegated}

	r.hosts.Store(e.Path, info)
	r.log.Debug("host registered",
		zap.String("host", e.Path),
		zap.Bool("delegated", delegated))

	r.sendAccept(e.Path)
}

func (r *HostRegistry) deleteHost(e events.Event) {
	if _, exists := r.hosts.LoadAndDelete(e.Path); !exists {
		r.log.Warn("host not found", zap.String("host", e.Path))
		r.sendReject(e.Path, "host not found")
		return
	}

	r.log.Debug("host removed", zap.String("host", e.Path))
	r.sendAccept(e.Path)
}

func (r *HostRegistry) sendAccept(path events.Path) {
	r.bus.Send(r.ctx, events.Event{
		System: process.HostSystem,
		Kind:   process.AcceptHost,
		Path:   path,
	})
}

func (r *HostRegistry) sendReject(path events.Path, reason string) {
	r.bus.Send(r.ctx, events.Event{
		System: process.HostSystem,
		Kind:   process.RejectHost,
		Path:   path,
		Data:   fmt.Errorf(reason),
	})
}

// GetHost returns a host and its type by ID
func (r *HostRegistry) GetHost(hostID string) (process.Host, bool) {
	if val, ok := r.hosts.Load(hostID); ok {
		info := val.(hostInfo)
		return info.host, true
	}
	return nil, false
}
