package engine

import (
	"context"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/relay"
)

// ResourcesKey is the context key for Resources in FrameContext
var ResourcesKey = &ctxapi.Key{Name: "engine.resources", Inherit: false}

// Resources holds process-local state accessible from context.
// Replaces UnitOfWork for the new scheduler model.
type Resources struct {
	mu       sync.Mutex
	channels *ChannelRegistry
	incoming []*relay.Package
	cleanups []*cleanupEntry
	closed   bool
}

// resourcesPool reuses Resources instances.
var resourcesPool = sync.Pool{
	New: func() any {
		return &Resources{
			channels: NewChannelRegistry(),
			incoming: make([]*relay.Package, 0, 4),
			cleanups: make([]*cleanupEntry, 0, 4),
		}
	},
}

// NewResources creates a new Resources instance from pool.
func NewResources() *Resources {
	r := resourcesPool.Get().(*Resources)
	r.closed = false
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
func SetResources(ctx context.Context, r *Resources) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(ResourcesKey, r)
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

// cleanupEntry wraps a cleanup function with cancellation support.
type cleanupEntry struct {
	fn        func() error
	cancelled bool
}

// AddCleanup registers a cleanup function to run on Close.
// Cleanups run in LIFO order.
// Returns a cancel function that removes this cleanup from the list.
// Call the cancel function when the resource is explicitly closed to prevent double-close.
func (r *Resources) AddCleanup(fn func() error) func() {
	entry := &cleanupEntry{fn: fn}
	r.mu.Lock()
	r.cleanups = append(r.cleanups, entry)
	r.mu.Unlock()

	return func() {
		r.mu.Lock()
		entry.cancelled = true
		r.mu.Unlock()
	}
}

// Close runs all cleanup functions and returns to pool.
// Safe to call multiple times - only first call has effect.
// Skips cancelled cleanups (resources that were explicitly closed).
func (r *Resources) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	cleanups := r.cleanups
	r.cleanups = r.cleanups[:0]
	r.incoming = r.incoming[:0]
	r.mu.Unlock()

	var lastErr error
	for i := len(cleanups) - 1; i >= 0; i-- {
		entry := cleanups[i]
		if entry.cancelled {
			continue
		}
		if err := entry.fn(); err != nil {
			lastErr = err
		}
	}

	// Reset channel registry and return to pool
	r.channels.Reset()
	resourcesPool.Put(r)

	return lastErr
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
