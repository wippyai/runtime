package engine

import (
	"context"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
)

// ResourcesKey is the context key for Resources in FrameContext
var ResourcesKey = &ctxapi.Key{Name: "engine.resources", Inherit: false}

// Resources holds process-local state for Lua engine execution.
// Manages message queue and wraps resource.Store for cleanup.
type Resources struct {
	mu       sync.Mutex
	store    *resource.Store
	incoming []*relay.Package
	closed   bool
}

// resourcesPool reuses Resources instances.
var resourcesPool = sync.Pool{
	New: func() any {
		return &Resources{
			store:    resource.NewStore(),
			incoming: make([]*relay.Package, 0, 4),
		}
	},
}

// NewResources creates a new Resources instance from pool.
func NewResources() *Resources {
	r := resourcesPool.Get().(*Resources)
	r.closed = false
	if r.store == nil {
		r.store = resource.NewStore()
	}
	return r
}

// GetResources retrieves Resources from FrameContext.
func GetResources(ctx context.Context) *Resources {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(ResourcesKey); ok {
		return val.(*Resources)
	}
	return nil
}

// SetResources stores Resources in FrameContext.
// Also sets the Store via resource.StoreKey for unified access.
func SetResources(ctx context.Context, r *Resources) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	if err := fc.Set(ResourcesKey, r); err != nil {
		return err
	}
	return fc.Set(resource.StoreKey, r.store)
}

// QueueMessage adds a message to the incoming queue.
func (r *Resources) QueueMessage(pkg *relay.Package) {
	r.mu.Lock()
	r.incoming = append(r.incoming, pkg)
	r.mu.Unlock()
}

// DrainMessages returns and clears all incoming messages.
func (r *Resources) DrainMessages() []*relay.Package {
	r.mu.Lock()
	msgs := r.incoming
	r.incoming = r.incoming[:0]
	r.mu.Unlock()
	return msgs
}

// Close runs all cleanup functions and returns to pool.
func (r *Resources) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.incoming = r.incoming[:0]
	r.mu.Unlock()

	err := r.store.Close()
	r.store = resource.NewStore()
	resourcesPool.Put(r)

	return err
}
