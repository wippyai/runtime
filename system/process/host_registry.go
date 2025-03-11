package process

import (
	"context"
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/api/event"
	api "github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
	"sync"
)

type hostInfo struct {
	host    api.Host
	managed bool
}

// HostRegistry manages process hosts and their topology
type HostRegistry struct {
	ctx        context.Context
	log        *zap.Logger
	bus        event.Bus
	hosts      sync.Map // map[string]hostInfo
	subscriber *eventbus.Subscriber
}

// NewHostRegistry creates a new host registry instance
func NewHostRegistry(bus event.Bus, logger *zap.Logger) *HostRegistry {
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
		api.HostSystem,
		"hosts.(register|delete)",
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

func (r *HostRegistry) handleEvent(e event.Event) {
	switch e.Kind {
	case api.HostRegister:
		r.registerHost(e)
	case api.HostDelete:
		r.deleteHost(e)
	default:
		r.log.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (r *HostRegistry) registerHost(e event.Event) {
	host, ok := e.Data.(api.Host)
	if !ok {
		r.log.Error("invalid host payload",
			zap.String("host", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		r.sendReject(e.Path, "invalid host data type")
		return
	}

	// Determine host type
	managed := false
	switch h := host.(type) {
	case api.Managed:
		managed = true
		_ = h // avoid unused variable warning
	case api.Delegated:
		_ = h // avoid unused variable warning
	default:
		r.log.Error("invalid host implementation",
			zap.String("host", e.Path),
			zap.String("type", fmt.Sprintf("%T", host)))
		r.sendReject(e.Path, "host must implement either Managed or Delegated interface")
		return
	}

	info := hostInfo{host: host, managed: managed}

	r.hosts.Store(e.Path, info)
	r.log.Debug("host registered",
		zap.String("host", e.Path),
		zap.Bool("managed", managed))

	r.sendAccept(e.Path)
}

func (r *HostRegistry) deleteHost(e event.Event) {
	if _, exists := r.hosts.LoadAndDelete(e.Path); !exists {
		r.log.Warn("host not found", zap.String("host", e.Path))
		r.sendReject(e.Path, "host not found")
		return
	}

	r.log.Debug("host removed", zap.String("host", e.Path))
	r.sendAccept(e.Path)
}

func (r *HostRegistry) sendAccept(path event.Path) {
	r.bus.Send(r.ctx, event.Event{
		System: api.HostSystem,
		Kind:   api.HostAccept,
		Path:   path,
	})
}

func (r *HostRegistry) sendReject(path event.Path, reason string) {
	r.bus.Send(r.ctx, event.Event{
		System: api.HostSystem,
		Kind:   api.HostReject,
		Path:   path,
		Data:   errors.New(reason),
	})
}

// GetHost returns a host and its type by Process
func (r *HostRegistry) GetHost(hostID string) (api.Host, bool) {
	if val, ok := r.hosts.Load(hostID); ok {
		info := val.(hostInfo)
		return info.host, true
	}
	return nil, false
}
