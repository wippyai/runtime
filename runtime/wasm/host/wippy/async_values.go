package wippy

import (
	"context"
	"sync"
)

type asyncValueStoreKey struct{}

// AsyncValueStore stores non-scalar dispatcher results for asyncify resume.
// Hosts receive only uint64 from resume, so complex values are tokenized here.
type AsyncValueStore struct {
	mu     sync.Mutex
	nextID uint64
	values map[uint64]any
}

// NewAsyncValueStore creates an empty async value store.
func NewAsyncValueStore() *AsyncValueStore {
	return &AsyncValueStore{
		nextID: 1, // 0 is reserved for "no value"
		values: make(map[uint64]any),
	}
}

// Put stores value and returns token ID.
func (s *AsyncValueStore) Put(v any) uint64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	id := s.nextID
	s.nextID++
	s.values[id] = v
	s.mu.Unlock()
	return id
}

// Take returns value by token and removes it from store.
func (s *AsyncValueStore) Take(id uint64) (any, bool) {
	if s == nil || id == 0 {
		return nil, false
	}
	s.mu.Lock()
	v, ok := s.values[id]
	if ok {
		delete(s.values, id)
	}
	s.mu.Unlock()
	return v, ok
}

// Reset clears all stored values.
func (s *AsyncValueStore) Reset() {
	if s == nil {
		return
	}
	s.mu.Lock()
	clear(s.values)
	s.nextID = 1
	s.mu.Unlock()
}

// WithAsyncValueStore attaches async value store to call context.
func WithAsyncValueStore(ctx context.Context, store *AsyncValueStore) context.Context {
	if store == nil {
		return ctx
	}
	return context.WithValue(ctx, asyncValueStoreKey{}, store)
}

// GetAsyncValueStore gets async value store from call context.
func GetAsyncValueStore(ctx context.Context) *AsyncValueStore {
	if ctx == nil {
		return nil
	}
	store, _ := ctx.Value(asyncValueStoreKey{}).(*AsyncValueStore)
	return store
}
