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

// Resources holds process-local state accessible from context.
// Uses resource.Store for unified handle-based resource management.
type Resources struct {
	mu       sync.Mutex
	store    *resource.Store
	channels *ChannelRegistry
	incoming []*relay.Package
	closed   bool
}

// resourcesPool reuses Resources instances.
var resourcesPool = sync.Pool{
	New: func() any {
		return &Resources{
			store:    resource.NewStore(),
			channels: NewChannelRegistry(),
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
// Also sets the Store via resource.StoreKey for dispatcher access.
func SetResources(ctx context.Context, r *Resources) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	if err := fc.Set(ResourcesKey, r); err != nil {
		return err
	}
	// Set Store via shared key so dispatchers can access it
	return fc.Set(resource.StoreKey, r.store)
}

// Store returns the resource store for handle-based access.
func (r *Resources) Store() *resource.Store {
	return r.store
}

// Table returns the underlying resource table for direct handle operations.
func (r *Resources) Table() *resource.Table {
	return r.store.Table()
}

// Channels returns the channel registry.
func (r *Resources) Channels() *ChannelRegistry {
	return r.channels
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

// AddCleanup registers a cleanup function to run on Close.
// Cleanups run in LIFO order.
// Returns a cancel function that prevents this cleanup from running.
func (r *Resources) AddCleanup(fn func() error) func() {
	return r.store.AddCleanup(fn)
}

// Close runs all cleanup functions and returns to pool.
// Safe to call multiple times - only first call has effect.
func (r *Resources) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.incoming = r.incoming[:0]
	r.mu.Unlock()

	// Close the store (runs all cleanups and closes table)
	err := r.store.Close()

	// Reset channel registry
	r.channels.Reset()

	// Get fresh store for pool reuse
	r.store = resource.NewStore()
	resourcesPool.Put(r)

	return err
}

// ChannelRegistry manages named channels for the process.
type ChannelRegistry struct {
	mu       sync.RWMutex
	channels map[string]*Channel
}

// NewChannelRegistry creates a new channel registry.
func NewChannelRegistry() *ChannelRegistry {
	return &ChannelRegistry{
		channels: make(map[string]*Channel),
	}
}

// Get returns a channel by name, creating it if needed.
func (r *ChannelRegistry) Get(name string) *Channel {
	r.mu.Lock()
	defer r.mu.Unlock()

	if ch, ok := r.channels[name]; ok {
		return ch
	}
	ch := NewChannel(0) // unbuffered by default
	ch.name = name
	r.channels[name] = ch
	return ch
}

// GetOrCreate returns existing channel or creates with given buffer size.
func (r *ChannelRegistry) GetOrCreate(name string, bufferSize int) *Channel {
	r.mu.Lock()
	defer r.mu.Unlock()

	if ch, ok := r.channels[name]; ok {
		return ch
	}
	ch := NewChannel(bufferSize)
	ch.name = name
	r.channels[name] = ch
	return ch
}

// Close closes all channels in the registry.
func (r *ChannelRegistry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, ch := range r.channels {
		ch.Close(nil)
	}
	r.channels = make(map[string]*Channel)
}

// Reset closes all channels and clears the map for reuse.
func (r *ChannelRegistry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, ch := range r.channels {
		ch.Close(nil)
		delete(r.channels, name)
	}
}
