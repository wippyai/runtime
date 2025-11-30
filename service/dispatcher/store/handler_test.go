package store

import (
	"context"
	"errors"
	"testing"

	storeapi "github.com/wippyai/runtime/api/dispatcher/store"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/store"
)

// mockPayload implements payload.Payload for testing
type mockPayload struct {
	data any
}

func (m *mockPayload) Format() payload.Format { return payload.Golang }
func (m *mockPayload) Data() any              { return m.data }

// mockStore implements store.Store for testing
type mockStore struct {
	data   map[string]payload.Payload
	getErr error
	setErr error
	delErr error
	hasErr error
}

func newMockStore() *mockStore {
	return &mockStore{data: make(map[string]payload.Payload)}
}

func (m *mockStore) Get(ctx context.Context, key registry.ID) (payload.Payload, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	v, ok := m.data[key.String()]
	if !ok {
		return nil, store.ErrKeyNotFound
	}
	return v, nil
}

func (m *mockStore) Set(ctx context.Context, entry store.Entry) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.data[entry.Key.String()] = entry.Value
	return nil
}

func (m *mockStore) Delete(ctx context.Context, key registry.ID) error {
	if m.delErr != nil {
		return m.delErr
	}
	if _, ok := m.data[key.String()]; !ok {
		return store.ErrKeyNotFound
	}
	delete(m.data, key.String())
	return nil
}

func (m *mockStore) Has(ctx context.Context, key registry.ID) (bool, error) {
	if m.hasErr != nil {
		return false, m.hasErr
	}
	_, ok := m.data[key.String()]
	return ok, nil
}

func TestStoreGetHandler(t *testing.T) {
	h := NewStoreGetHandler()
	s := newMockStore()
	key := registry.NewID("ns", "key1")
	value := &mockPayload{data: "hello"}
	s.data[key.String()] = value

	var resp storeapi.StoreGetResponse
	err := h.Handle(context.Background(), &storeapi.StoreGetCmd{
		Store: s,
		Key:   key,
	}, func(data any) {
		resp = data.(storeapi.StoreGetResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Error != nil {
		t.Errorf("unexpected response error: %v", resp.Error)
	}
	if resp.Value == nil {
		t.Fatal("expected value, got nil")
	}
	if resp.Value.Data() != "hello" {
		t.Errorf("expected 'hello', got %v", resp.Value.Data())
	}
}

func TestStoreGetHandlerNotFound(t *testing.T) {
	h := NewStoreGetHandler()
	s := newMockStore()
	key := registry.NewID("ns", "missing")

	var resp storeapi.StoreGetResponse
	err := h.Handle(context.Background(), &storeapi.StoreGetCmd{
		Store: s,
		Key:   key,
	}, func(data any) {
		resp = data.(storeapi.StoreGetResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Error != store.ErrKeyNotFound {
		t.Errorf("expected ErrKeyNotFound, got %v", resp.Error)
	}
}

func TestStoreSetHandler(t *testing.T) {
	h := NewStoreSetHandler()
	s := newMockStore()
	key := registry.NewID("ns", "key1")
	value := &mockPayload{data: "test"}

	var resp storeapi.StoreSetResponse
	err := h.Handle(context.Background(), &storeapi.StoreSetCmd{
		Store: s,
		Entry: store.Entry{Key: key, Value: value},
	}, func(data any) {
		resp = data.(storeapi.StoreSetResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Error != nil {
		t.Errorf("unexpected response error: %v", resp.Error)
	}
	if s.data[key.String()] == nil {
		t.Error("value not stored")
	}
}

func TestStoreSetHandlerError(t *testing.T) {
	h := NewStoreSetHandler()
	s := newMockStore()
	s.setErr = errors.New("set failed")
	key := registry.NewID("ns", "key1")
	value := &mockPayload{data: "test"}

	var resp storeapi.StoreSetResponse
	err := h.Handle(context.Background(), &storeapi.StoreSetCmd{
		Store: s,
		Entry: store.Entry{Key: key, Value: value},
	}, func(data any) {
		resp = data.(storeapi.StoreSetResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Error == nil {
		t.Error("expected error")
	}
}

func TestStoreDeleteHandler(t *testing.T) {
	h := NewStoreDeleteHandler()
	s := newMockStore()
	key := registry.NewID("ns", "key1")
	s.data[key.String()] = &mockPayload{data: "test"}

	var resp storeapi.StoreDeleteResponse
	err := h.Handle(context.Background(), &storeapi.StoreDeleteCmd{
		Store: s,
		Key:   key,
	}, func(data any) {
		resp = data.(storeapi.StoreDeleteResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Error != nil {
		t.Errorf("unexpected response error: %v", resp.Error)
	}
	if _, ok := s.data[key.String()]; ok {
		t.Error("value not deleted")
	}
}

func TestStoreDeleteHandlerNotFound(t *testing.T) {
	h := NewStoreDeleteHandler()
	s := newMockStore()
	key := registry.NewID("ns", "missing")

	var resp storeapi.StoreDeleteResponse
	err := h.Handle(context.Background(), &storeapi.StoreDeleteCmd{
		Store: s,
		Key:   key,
	}, func(data any) {
		resp = data.(storeapi.StoreDeleteResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Error != store.ErrKeyNotFound {
		t.Errorf("expected ErrKeyNotFound, got %v", resp.Error)
	}
}

func TestStoreHasHandler(t *testing.T) {
	h := NewStoreHasHandler()
	s := newMockStore()
	key := registry.NewID("ns", "key1")
	s.data[key.String()] = &mockPayload{data: "test"}

	var resp storeapi.StoreHasResponse
	err := h.Handle(context.Background(), &storeapi.StoreHasCmd{
		Store: s,
		Key:   key,
	}, func(data any) {
		resp = data.(storeapi.StoreHasResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Error != nil {
		t.Errorf("unexpected response error: %v", resp.Error)
	}
	if !resp.Exists {
		t.Error("expected exists=true")
	}
}

func TestStoreHasHandlerNotFound(t *testing.T) {
	h := NewStoreHasHandler()
	s := newMockStore()
	key := registry.NewID("ns", "missing")

	var resp storeapi.StoreHasResponse
	err := h.Handle(context.Background(), &storeapi.StoreHasCmd{
		Store: s,
		Key:   key,
	}, func(data any) {
		resp = data.(storeapi.StoreHasResponse)
	})

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if resp.Error != nil {
		t.Errorf("unexpected response error: %v", resp.Error)
	}
	if resp.Exists {
		t.Error("expected exists=false")
	}
}

func TestStoreService(t *testing.T) {
	svc := NewService()
	if svc.Get == nil {
		t.Error("Get handler not initialized")
	}
	if svc.Set == nil {
		t.Error("Set handler not initialized")
	}
	if svc.Delete == nil {
		t.Error("Delete handler not initialized")
	}
	if svc.Has == nil {
		t.Error("Has handler not initialized")
	}
}
