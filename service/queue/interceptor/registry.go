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
	priority    int
	name        string
}

// sealedChain holds the immutable interceptor slice after rebuild
type sealedChain struct {
	interceptors []queueapi.PublishInterceptor
	publishFunc  func(context.Context, registry.ID, ...*queueapi.Message) error
}

// Registry manages publish interceptors.
// Uses atomic pointer for lock-free Publish() on hot path.
type Registry struct {
	logger      *zap.Logger
	entries     []entry
	publishFunc func(context.Context, registry.ID, ...*queueapi.Message) error
	mu          sync.Mutex
	chain       atomic.Pointer[sealedChain]
}

func NewRegistry(logger *zap.Logger) *Registry {
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

// rebuild creates the sealed chain (called with lock held)
func (r *Registry) rebuild() {
	sort.Slice(r.entries, func(i, j int) bool {
		return r.entries[i].priority < r.entries[j].priority
	})

	if len(r.entries) == 0 && r.publishFunc == nil {
		r.chain.Store(nil)
		return
	}

	interceptors := make([]queueapi.PublishInterceptor, len(r.entries))
	for i, e := range r.entries {
		interceptors[i] = e.interceptor
	}

	r.chain.Store(&sealedChain{
		interceptors: interceptors,
		publishFunc:  r.publishFunc,
	})
}

// Publish executes the interceptor chain. Lock-free on hot path.
func (r *Registry) Publish(ctx context.Context, queue registry.ID, msgs ...*queueapi.Message) error {
	chain := r.chain.Load()
	if chain == nil {
		return queueapi.ErrNoPublishFunc
	}

	if len(chain.interceptors) == 0 {
		if chain.publishFunc != nil {
			return chain.publishFunc(ctx, queue, msgs...)
		}
		return queueapi.ErrNoPublishFunc
	}

	return r.publishAt(ctx, queue, msgs, chain, 0)
}

// publishAt runs interceptor at index i
func (r *Registry) publishAt(ctx context.Context, queue registry.ID, msgs []*queueapi.Message, chain *sealedChain, i int) error {
	if i >= len(chain.interceptors) {
		if chain.publishFunc != nil {
			return chain.publishFunc(ctx, queue, msgs...)
		}
		return queueapi.ErrNoPublishFunc
	}

	return chain.interceptors[i].Handle(ctx, queue, msgs, func(ctx context.Context, q registry.ID, m []*queueapi.Message) error {
		return r.publishAt(ctx, q, m, chain, i+1)
	})
}
