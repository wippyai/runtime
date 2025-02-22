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
	defaultFS   fsapi.FS // Default filesystem for fallback
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
	case fsapi.RegisterDefault:
		r.registerDefaultFS(e)
	case fsapi.DeleteDefault:
		r.deleteDefaultFS(e)
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

func (r *Registry) registerDefaultFS(e events.Event) {
	fs, ok := e.Data.(fsapi.FS)
	if !ok {
		r.log.Error("invalid default filesystem payload",
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		r.sendReject(e.Path, "invalid filesystem data type")
		return
	}

	r.defaultFS = fs
	r.log.Debug("default filesystem registered")
	r.sendAccept(e.Path)
}

func (r *Registry) deleteDefaultFS(e events.Event) {
	if r.defaultFS == nil {
		r.log.Warn("no default filesystem to delete")
		r.sendReject(e.Path, "no default filesystem")
		return
	}

	r.defaultFS = nil
	r.log.Debug("default filesystem removed")
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

// GetDefaultFS returns the default filesystem if set
func (r *Registry) GetDefaultFS() (fsapi.FS, bool) {
	if r.defaultFS != nil {
		return r.defaultFS, true
	}

	return nil, false
}
