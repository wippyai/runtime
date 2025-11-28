package clock

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	clockapi "github.com/wippyai/runtime/api/dispatcher/clock"
)

// TickerRegistryKey is the context key for TickerRegistry.
var TickerRegistryKey = &ctxapi.Key{Name: "clock.tickers", Inherit: false}

// ErrTickerNotFound is returned when ticker ID doesn't exist.
var ErrTickerNotFound = errors.New("ticker not found")

// ErrTickerClosed is returned when ticker was already stopped.
var ErrTickerClosed = errors.New("ticker closed")

// tickerEntry holds an active ticker and its channel.
type tickerEntry struct {
	ticker *time.Ticker
	ch     <-chan time.Time
	closed atomic.Bool
}

// TickerRegistry manages active tickers for a process.
// Thread-safe, stores tickers by uint64 ID.
type TickerRegistry struct {
	mu      sync.Mutex
	tickers map[uint64]*tickerEntry
	nextID  uint64
}

// NewTickerRegistry creates a new ticker registry.
func NewTickerRegistry() *TickerRegistry {
	return &TickerRegistry{
		tickers: make(map[uint64]*tickerEntry),
	}
}

// Start creates a new ticker with given duration, returns its ID.
func (r *TickerRegistry) Start(d time.Duration) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextID++
	id := r.nextID

	t := time.NewTicker(d)
	r.tickers[id] = &tickerEntry{
		ticker: t,
		ch:     t.C,
	}

	return id
}

// Next waits for the next tick from ticker with given ID.
// Returns tick time or error if ticker not found or closed.
func (r *TickerRegistry) Next(ctx context.Context, id uint64) (time.Time, error) {
	r.mu.Lock()
	entry, ok := r.tickers[id]
	r.mu.Unlock()

	if !ok {
		return time.Time{}, ErrTickerNotFound
	}

	if entry.closed.Load() {
		return time.Time{}, ErrTickerClosed
	}

	select {
	case <-ctx.Done():
		return time.Time{}, ctx.Err()
	case t, ok := <-entry.ch:
		if !ok {
			return time.Time{}, ErrTickerClosed
		}
		return t, nil
	}
}

// Stop stops and removes ticker with given ID.
func (r *TickerRegistry) Stop(id uint64) error {
	r.mu.Lock()
	entry, ok := r.tickers[id]
	if ok {
		delete(r.tickers, id)
	}
	r.mu.Unlock()

	if !ok {
		return ErrTickerNotFound
	}

	entry.closed.Store(true)
	entry.ticker.Stop()
	return nil
}

// Close stops all tickers and clears the registry.
func (r *TickerRegistry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, entry := range r.tickers {
		entry.closed.Store(true)
		entry.ticker.Stop()
		delete(r.tickers, id)
	}
}

// GetTickerRegistry retrieves TickerRegistry from FrameContext.
func GetTickerRegistry(ctx context.Context) *TickerRegistry {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(TickerRegistryKey); ok {
		return val.(*TickerRegistry)
	}
	return nil
}

// SetTickerRegistry stores TickerRegistry in FrameContext.
func SetTickerRegistry(ctx context.Context, r *TickerRegistry) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(TickerRegistryKey, r)
}

// GetOrCreateTickerRegistry returns existing registry or creates a new one.
func GetOrCreateTickerRegistry(ctx context.Context) *TickerRegistry {
	if r := GetTickerRegistry(ctx); r != nil {
		return r
	}
	r := NewTickerRegistry()
	SetTickerRegistry(ctx, r)
	return r
}

// TickerStartHandler creates a new ticker and returns its ID.
type TickerStartHandler struct{}

func NewTickerStartHandler() *TickerStartHandler {
	return &TickerStartHandler{}
}

func (h *TickerStartHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	startCmd := cmd.(clockapi.TickerStartCmd)

	if startCmd.Duration <= 0 {
		return errors.New("ticker duration must be positive")
	}

	registry := GetOrCreateTickerRegistry(ctx)
	id := registry.Start(startCmd.Duration)
	emit(id)

	return nil
}

// TickerNextHandler waits for the next tick from a ticker.
type TickerNextHandler struct{}

func NewTickerNextHandler() *TickerNextHandler {
	return &TickerNextHandler{}
}

func (h *TickerNextHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	nextCmd := cmd.(clockapi.TickerNextCmd)

	registry := GetTickerRegistry(ctx)
	if registry == nil {
		return ErrTickerNotFound
	}

	t, err := registry.Next(ctx, nextCmd.TickerID)
	if err != nil {
		return err
	}

	emit(t.UnixNano())
	return nil
}

// TickerStopHandler stops and removes a ticker.
type TickerStopHandler struct{}

func NewTickerStopHandler() *TickerStopHandler {
	return &TickerStopHandler{}
}

func (h *TickerStopHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	stopCmd := cmd.(clockapi.TickerStopCmd)

	registry := GetTickerRegistry(ctx)
	if registry == nil {
		return ErrTickerNotFound
	}

	return registry.Stop(stopCmd.TickerID)
}
