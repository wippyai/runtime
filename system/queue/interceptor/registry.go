package interceptor

import (
	"context"
	"sort"
	"sync"

	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

type entry struct {
	interceptor queueapi.PublishInterceptor
	priority    int
	name        string
}

type Registry struct {
	logger      *zap.Logger
	entries     []entry
	chain       queueapi.PublishChain
	publishFunc func(context.Context, registry.ID, ...*queueapi.Message) error
	mu          sync.RWMutex
}

func NewInterceptorRegistry(logger *zap.Logger) *Registry {
	return &Registry{
		logger:  logger,
		entries: make([]entry, 0),
	}
}

func (r *Registry) Register(name string, interceptor queueapi.PublishInterceptor, priority int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range r.entries {
		if e.name == name {
			r.logger.Warn("interceptor already registered, skipping",
				zap.String("name", name))
			return
		}
	}

	r.entries = append(r.entries, entry{
		interceptor: interceptor,
		priority:    priority,
		name:        name,
	})

	r.rebuild()

	r.logger.Debug("interceptor registered",
		zap.String("name", name),
		zap.Int("priority", priority))
}

func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, e := range r.entries {
		if e.name == name {
			r.entries = append(r.entries[:i], r.entries[i+1:]...)
			r.rebuild()
			r.logger.Debug("interceptor unregistered", zap.String("name", name))
			return
		}
	}

	r.logger.Warn("interceptor not found for unregister", zap.String("name", name))
}

func (r *Registry) SetPublishFunc(f func(context.Context, registry.ID, ...*queueapi.Message) error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.publishFunc = f
	r.rebuild()
}

func (r *Registry) rebuild() {
	sort.Slice(r.entries, func(i, j int) bool {
		return r.entries[i].priority < r.entries[j].priority
	})

	interceptors := make([]queueapi.PublishInterceptor, len(r.entries))
	for i, e := range r.entries {
		interceptors[i] = e.interceptor
	}

	chain := newChain(interceptors, r.publishFunc, r.logger)
	r.chain = &chain
}

func (r *Registry) Publish(ctx context.Context, queue registry.ID, msgs ...*queueapi.Message) error {
	r.mu.RLock()
	chain := r.chain
	r.mu.RUnlock()

	if chain == nil {
		if r.publishFunc != nil {
			return r.publishFunc(ctx, queue, msgs...)
		}
		return queueapi.ErrNoPublishFunc
	}

	return chain.Publish(ctx, queue, msgs...)
}
