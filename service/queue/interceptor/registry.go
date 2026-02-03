package interceptor

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"

	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

type entry struct {
	interceptor queueapi.PublishInterceptor
	name        string
	priority    int
}

type sealedChain struct {
	publishFunc  func(context.Context, registry.ID, ...*queueapi.Message) error
	interceptors []queueapi.PublishInterceptor
}

type Registry struct {
	logger      *zap.Logger
	publishFunc func(context.Context, registry.ID, ...*queueapi.Message) error
	chain       atomic.Pointer[sealedChain]
	entries     []entry
	mu          sync.Mutex
}

func NewRegistry(logger *zap.Logger, publishFunc func(context.Context, registry.ID, ...*queueapi.Message) error) *Registry {
	r := &Registry{
		logger:      logger,
		entries:     make([]entry, 0),
		publishFunc: publishFunc,
	}
	r.rebuild()
	return r
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

func (r *Registry) rebuild() {
	sort.Slice(r.entries, func(i, j int) bool {
		return r.entries[i].priority < r.entries[j].priority
	})

	interceptors := make([]queueapi.PublishInterceptor, len(r.entries))
	for i, e := range r.entries {
		interceptors[i] = e.interceptor
	}

	r.chain.Store(&sealedChain{
		interceptors: interceptors,
		publishFunc:  r.publishFunc,
	})
}

func (r *Registry) Publish(ctx context.Context, queue registry.ID, msgs ...*queueapi.Message) error {
	chain := r.chain.Load()

	if len(chain.interceptors) == 0 {
		return chain.publishFunc(ctx, queue, msgs...)
	}

	return r.publishAt(ctx, queue, msgs, chain, 0)
}

func (r *Registry) publishAt(ctx context.Context, queue registry.ID, msgs []*queueapi.Message, chain *sealedChain, i int) error {
	if i >= len(chain.interceptors) {
		return chain.publishFunc(ctx, queue, msgs...)
	}

	return chain.interceptors[i].Handle(ctx, queue, msgs, func(ctx context.Context, q registry.ID, m []*queueapi.Message) error {
		return r.publishAt(ctx, q, m, chain, i+1)
	})
}
