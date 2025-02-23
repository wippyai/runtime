// fs_registry.go
package fs

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	fsapi "github.com/ponyruntime/pony/api/fs"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
	"sync"
)

// Registry manages filesystem mounts and their registration
type Registry struct {
	ctx         context.Context
	log         *zap.Logger
	bus         events.Bus
	filesystems sync.Map // map[string]FS
	subscriber  *eventbus.Subscriber
}

// NewFSRegistry creates a new filesystem registry instance
func NewFSRegistry(bus events.Bus, log *zap.Logger) *Registry {
	return &Registry{
		log: log,
		bus: bus,
	}
}

// Start begins listening for filesystem registration events
func (r *Registry) Start(ctx context.Context) error {
	r.ctx = ctx

	sub, err := eventbus.NewSubscriber(r.ctx, r.bus, fsapi.System, "fs.*", r.handleEvent)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	r.subscriber = sub

	return nil
}

// Stop cleans up registry resources
func (r *Registry) Stop() error {
	if r.subscriber != nil {
		r.subscriber.Close()
	}
	return nil
}

func (r *Registry) handleEvent(e events.Event) {
	switch e.Kind {
	case fsapi.Register:
		r.registerFS(e)
	case fsapi.Delete:
		r.deleteFS(e)
	case fsapi.Accept, fsapi.Reject:
		// nothing, self emitted
	default:
		r.log.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (r *Registry) registerFS(e events.Event) {
	fs, ok := e.Data.(fsapi.FS)
	if !ok {
		r.log.Error("invalid filesystem payload",
			zap.String("fs", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		r.sendReject(e.Path, "invalid filesystem data type")
		return
	}

	r.filesystems.Store(e.Path, fs)
	r.log.Debug("filesystem registered", zap.String("fs", e.Path))
	r.sendAccept(e.Path)
}

func (r *Registry) deleteFS(e events.Event) {
	if _, exists := r.filesystems.LoadAndDelete(e.Path); !exists {
		r.log.Warn("filesystem not found", zap.String("fs", e.Path))
		r.sendReject(e.Path, "filesystem not found")
		return
	}

	r.log.Debug("filesystem removed", zap.String("fs", e.Path))
	r.sendAccept(e.Path)
}

func (r *Registry) sendAccept(path events.Path) {
	r.bus.Send(r.ctx, events.Event{
		System: fsapi.System,
		Kind:   fsapi.Accept,
		Path:   path,
	})
}

func (r *Registry) sendReject(path events.Path, reason string) {
	r.bus.Send(r.ctx, events.Event{
		System: fsapi.System,
		Kind:   fsapi.Reject,
		Path:   path,
		Data:   reason,
	})
}

// GetFS returns a filesystem by path
func (r *Registry) GetFS(path string) (fsapi.FS, bool) {
	if val, ok := r.filesystems.Load(path); ok {
		return val.(fsapi.FS), true
	}
	return nil, false
}
