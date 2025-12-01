package clock

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/resource"
)

// AfterRegistryKey is the context key for AfterRegistry.
var AfterRegistryKey = &ctxapi.Key{Name: "clock.after", Inherit: false}

// afterEntry holds a pending timer channel.
type afterEntry struct {
	timer  *time.Timer
	cancel context.CancelFunc
	closed atomic.Bool
}

// AfterRegistry manages pending after timers for a process.
type AfterRegistry struct {
	mu      sync.Mutex
	entries map[uint64]*afterEntry
	nextID  uint64
}

// NewAfterRegistry creates a new after registry.
func NewAfterRegistry() *AfterRegistry {
	return &AfterRegistry{
		entries: make(map[uint64]*afterEntry),
	}
}

// Create creates a new timer that will fire after duration.
// Returns the channel ID immediately.
func (r *AfterRegistry) Create(ctx context.Context, d time.Duration) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextID++
	id := r.nextID

	timerCtx, cancel := context.WithCancel(ctx)
	timer := time.NewTimer(d)

	entry := &afterEntry{
		timer:  timer,
		cancel: cancel,
	}
	r.entries[id] = entry

	// Background goroutine to handle timer or context cancellation
	go func() {
		select {
		case <-timer.C:
			// Timer fired - channel send happens at Lua level
		case <-timerCtx.Done():
			timer.Stop()
		}

		r.mu.Lock()
		delete(r.entries, id)
		r.mu.Unlock()
	}()

	return id
}

// Close cancels all pending timers.
func (r *AfterRegistry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, entry := range r.entries {
		entry.closed.Store(true)
		entry.cancel()
		entry.timer.Stop()
		delete(r.entries, id)
	}
}

// GetAfterRegistry retrieves AfterRegistry from FrameContext.
func GetAfterRegistry(ctx context.Context) *AfterRegistry {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(AfterRegistryKey); ok {
		return val.(*AfterRegistry)
	}
	return nil
}

// SetAfterRegistry stores AfterRegistry in FrameContext.
func SetAfterRegistry(ctx context.Context, r *AfterRegistry) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(AfterRegistryKey, r)
}

// GetOrCreateAfterRegistry returns existing registry or creates a new one.
func GetOrCreateAfterRegistry(ctx context.Context) *AfterRegistry {
	if r := GetAfterRegistry(ctx); r != nil {
		return r
	}
	r := NewAfterRegistry()
	_ = SetAfterRegistry(ctx, r)

	if store := resource.GetStore(ctx); store != nil {
		store.AddCleanup(func() error {
			r.Close()
			return nil
		})
	}

	return r
}
