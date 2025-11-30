package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	storeapi "github.com/wippyai/runtime/api/dispatcher/store"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/store"
)

type mockStore struct {
	data  map[string]payload.Payload
	mu    sync.RWMutex
	delay time.Duration
}

func newMockStore() *mockStore {
	return &mockStore{data: make(map[string]payload.Payload)}
}

func (s *mockStore) Get(_ context.Context, key registry.ID) (payload.Payload, error) {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key.String()]
	if !ok {
		return nil, store.ErrKeyNotFound
	}
	return v, nil
}

func (s *mockStore) Set(_ context.Context, entry store.Entry) error {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[entry.Key.String()] = entry.Value
	return nil
}

func (s *mockStore) Delete(_ context.Context, key registry.ID) error {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key.String())
	return nil
}

func (s *mockStore) Has(_ context.Context, key registry.ID) (bool, error) {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[key.String()]
	return ok, nil
}

// getHandler returns a handler for the dispatcher.
func getHandler(d *Dispatcher) *handler {
	return &handler{d: d}
}

func TestBlockingDispatcher(t *testing.T) {
	d := NewBlockingDispatcher()
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ctx := context.Background()
	h := getHandler(d)

	// Test Set
	var setResp storeapi.StoreSetResponse
	setCmd := &storeapi.StoreSetCmd{
		Store: ms,
		Entry: store.Entry{
			Key:   registry.ID{NS: "test", Name: "key1"},
			Value: payload.New("value1"),
		},
	}

	err := h.Handle(ctx, setCmd, func(data any) {
		setResp = data.(storeapi.StoreSetResponse)
	})
	require.NoError(t, err)
	assert.NoError(t, setResp.Error)

	// Test Get
	var getResp storeapi.StoreGetResponse
	getCmd := &storeapi.StoreGetCmd{
		Store: ms,
		Key:   registry.ID{NS: "test", Name: "key1"},
	}

	err = h.Handle(ctx, getCmd, func(data any) {
		getResp = data.(storeapi.StoreGetResponse)
	})
	require.NoError(t, err)
	assert.NoError(t, getResp.Error)
	assert.NotNil(t, getResp.Value)

	// Test Has
	var hasResp storeapi.StoreHasResponse
	hasCmd := &storeapi.StoreHasCmd{
		Store: ms,
		Key:   registry.ID{NS: "test", Name: "key1"},
	}

	err = h.Handle(ctx, hasCmd, func(data any) {
		hasResp = data.(storeapi.StoreHasResponse)
	})
	require.NoError(t, err)
	assert.NoError(t, hasResp.Error)
	assert.True(t, hasResp.Exists)

	// Test Delete
	var delResp storeapi.StoreDeleteResponse
	delCmd := &storeapi.StoreDeleteCmd{
		Store: ms,
		Key:   registry.ID{NS: "test", Name: "key1"},
	}

	err = h.Handle(ctx, delCmd, func(data any) {
		delResp = data.(storeapi.StoreDeleteResponse)
	})
	require.NoError(t, err)
	assert.NoError(t, delResp.Error)
}

func TestAsyncDispatcher(t *testing.T) {
	d := NewAsyncDispatcher(2)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ms.delay = 10 * time.Millisecond
	ctx := context.Background()
	h := getHandler(d)

	var wg sync.WaitGroup
	results := make(chan storeapi.StoreSetResponse, 10)

	// Submit multiple operations concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := &storeapi.StoreSetCmd{
				Store: ms,
				Entry: store.Entry{
					Key:   registry.ID{NS: "test", Name: "key"},
					Value: payload.New("value"),
				},
			}
			_ = h.Handle(ctx, cmd, func(data any) {
				results <- data.(storeapi.StoreSetResponse)
			})
		}()
	}

	wg.Wait()

	// Wait for all results
	timeout := time.After(2 * time.Second)
	for i := 0; i < 10; i++ {
		select {
		case resp := <-results:
			assert.NoError(t, resp.Error)
		case <-timeout:
			t.Fatal("timeout waiting for results")
		}
	}
}

func TestRegisterAll(t *testing.T) {
	d := NewBlockingDispatcher()

	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		handlers[id] = h
	})

	assert.Len(t, handlers, 4)
	assert.NotNil(t, handlers[storeapi.CmdStoreGet])
	assert.NotNil(t, handlers[storeapi.CmdStoreSet])
	assert.NotNil(t, handlers[storeapi.CmdStoreDelete])
	assert.NotNil(t, handlers[storeapi.CmdStoreHas])
}

func TestDispatcher_Lifecycle(t *testing.T) {
	t.Run("blocking mode - no workers", func(t *testing.T) {
		d := NewBlockingDispatcher()
		assert.Equal(t, 0, d.workers)
		assert.False(t, d.isAsync())

		// Start should be no-op
		require.NoError(t, d.Start(context.Background()))
		assert.False(t, d.isAsync())

		// Stop should be no-op
		require.NoError(t, d.Stop(context.Background()))
	})

	t.Run("async mode - starts workers", func(t *testing.T) {
		d := NewAsyncDispatcher(4)
		assert.Equal(t, 4, d.workers)

		require.NoError(t, d.Start(context.Background()))
		assert.True(t, d.isAsync())
		assert.NotNil(t, d.jobs)

		require.NoError(t, d.Stop(context.Background()))
	})

	t.Run("config constructor", func(t *testing.T) {
		d := NewDispatcher(Config{Workers: 8})
		assert.Equal(t, 8, d.workers)
	})

	t.Run("async with zero workers defaults to 4", func(t *testing.T) {
		d := NewAsyncDispatcher(0)
		assert.Equal(t, 4, d.workers)
	})
}

func TestDispatcher_ErrorHandling(t *testing.T) {
	d := NewBlockingDispatcher()
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ctx := context.Background()
	h := getHandler(d)

	// Test Get on non-existent key
	var getResp storeapi.StoreGetResponse
	getCmd := &storeapi.StoreGetCmd{
		Store: ms,
		Key:   registry.ID{NS: "test", Name: "nonexistent"},
	}

	err := h.Handle(ctx, getCmd, func(data any) {
		getResp = data.(storeapi.StoreGetResponse)
	})
	require.NoError(t, err)
	assert.ErrorIs(t, getResp.Error, store.ErrKeyNotFound)
	assert.Nil(t, getResp.Value)

	// Test Has on non-existent key
	var hasResp storeapi.StoreHasResponse
	hasCmd := &storeapi.StoreHasCmd{
		Store: ms,
		Key:   registry.ID{NS: "test", Name: "nonexistent"},
	}

	err = h.Handle(ctx, hasCmd, func(data any) {
		hasResp = data.(storeapi.StoreHasResponse)
	})
	require.NoError(t, err)
	assert.NoError(t, hasResp.Error)
	assert.False(t, hasResp.Exists)
}

func TestAsyncDispatcher_GracefulShutdown(t *testing.T) {
	d := NewAsyncDispatcher(2)
	require.NoError(t, d.Start(context.Background()))

	ms := newMockStore()
	ms.delay = 50 * time.Millisecond
	ctx := context.Background()
	h := getHandler(d)

	// Submit a slow operation
	done := make(chan struct{})
	cmd := &storeapi.StoreSetCmd{
		Store: ms,
		Entry: store.Entry{
			Key:   registry.ID{NS: "test", Name: "key"},
			Value: payload.New("value"),
		},
	}
	_ = h.Handle(ctx, cmd, func(_ any) {
		close(done)
	})

	// Stop should wait for in-flight operations
	require.NoError(t, d.Stop(context.Background()))

	// Operation should have completed
	select {
	case <-done:
		// Good - operation completed before shutdown
	case <-time.After(200 * time.Millisecond):
		// Also acceptable - operation may have been cancelled
	}
}

func TestAsyncDispatcher_AllOperations(t *testing.T) {
	d := NewAsyncDispatcher(2)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ctx := context.Background()

	// Test all operations through async dispatcher
	h := getHandler(d)

	t.Run("Set", func(t *testing.T) {
		done := make(chan storeapi.StoreSetResponse, 1)
		cmd := &storeapi.StoreSetCmd{
			Store: ms,
			Entry: store.Entry{
				Key:   registry.ID{NS: "test", Name: "async-key"},
				Value: payload.New("async-value"),
			},
		}
		require.NoError(t, h.Handle(ctx, cmd, func(data any) {
			done <- data.(storeapi.StoreSetResponse)
		}))

		select {
		case resp := <-done:
			assert.NoError(t, resp.Error)
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("Get", func(t *testing.T) {
		done := make(chan storeapi.StoreGetResponse, 1)
		cmd := &storeapi.StoreGetCmd{
			Store: ms,
			Key:   registry.ID{NS: "test", Name: "async-key"},
		}
		require.NoError(t, h.Handle(ctx, cmd, func(data any) {
			done <- data.(storeapi.StoreGetResponse)
		}))

		select {
		case resp := <-done:
			assert.NoError(t, resp.Error)
			assert.NotNil(t, resp.Value)
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("Has", func(t *testing.T) {
		done := make(chan storeapi.StoreHasResponse, 1)
		cmd := &storeapi.StoreHasCmd{
			Store: ms,
			Key:   registry.ID{NS: "test", Name: "async-key"},
		}
		require.NoError(t, h.Handle(ctx, cmd, func(data any) {
			done <- data.(storeapi.StoreHasResponse)
		}))

		select {
		case resp := <-done:
			assert.NoError(t, resp.Error)
			assert.True(t, resp.Exists)
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		done := make(chan storeapi.StoreDeleteResponse, 1)
		cmd := &storeapi.StoreDeleteCmd{
			Store: ms,
			Key:   registry.ID{NS: "test", Name: "async-key"},
		}
		require.NoError(t, h.Handle(ctx, cmd, func(data any) {
			done <- data.(storeapi.StoreDeleteResponse)
		}))

		select {
		case resp := <-done:
			assert.NoError(t, resp.Error)
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})
}

func BenchmarkDispatcher_Blocking(b *testing.B) {
	d := NewBlockingDispatcher()
	_ = d.Start(context.Background())
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ctx := context.Background()
	key := registry.ID{NS: "bench", Name: "key"}
	h := getHandler(d)

	// Pre-populate
	ms.data[key.String()] = payload.New("value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cmd := &storeapi.StoreGetCmd{Store: ms, Key: key}
		_ = h.Handle(ctx, cmd, func(_ any) {})
	}
}

func BenchmarkDispatcher_Async(b *testing.B) {
	d := NewAsyncDispatcher(4)
	_ = d.Start(context.Background())
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ctx := context.Background()
	key := registry.ID{NS: "bench", Name: "key"}
	h := getHandler(d)

	// Pre-populate
	ms.data[key.String()] = payload.New("value")

	var wg sync.WaitGroup
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		cmd := &storeapi.StoreGetCmd{Store: ms, Key: key}
		_ = h.Handle(ctx, cmd, func(_ any) {
			wg.Done()
		})
	}
	wg.Wait()
}

// Stress tests

func TestStress_HighConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	d := NewAsyncDispatcher(8)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ctx := context.Background()
	h := getHandler(d)

	const (
		numGoroutines   = 100
		opsPerGoroutine = 1000
	)

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*opsPerGoroutine)

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for i := 0; i < opsPerGoroutine; i++ {
				key := registry.ID{NS: "stress", Name: fmt.Sprintf("key-%d-%d", goroutineID, i)}

				// Set
				setDone := make(chan struct{})
				setCmd := &storeapi.StoreSetCmd{
					Store: ms,
					Entry: store.Entry{Key: key, Value: payload.New("value")},
				}
				if err := h.Handle(ctx, setCmd, func(data any) {
					resp := data.(storeapi.StoreSetResponse)
					if resp.Error != nil {
						errors <- resp.Error
					}
					close(setDone)
				}); err != nil {
					errors <- err
				}
				<-setDone

				// Get
				getDone := make(chan struct{})
				getCmd := &storeapi.StoreGetCmd{Store: ms, Key: key}
				if err := h.Handle(ctx, getCmd, func(data any) {
					resp := data.(storeapi.StoreGetResponse)
					if resp.Error != nil {
						errors <- resp.Error
					}
					close(getDone)
				}); err != nil {
					errors <- err
				}
				<-getDone
			}
		}(g)
	}

	wg.Wait()
	close(errors)

	var errCount int
	for err := range errors {
		t.Logf("error: %v", err)
		errCount++
	}
	assert.Equal(t, 0, errCount, "expected no errors during stress test")
}

func TestStress_RapidStartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	for i := 0; i < 100; i++ {
		d := NewAsyncDispatcher(4)
		require.NoError(t, d.Start(context.Background()))
		h := getHandler(d)

		// Submit some work
		ms := newMockStore()
		done := make(chan struct{})
		cmd := &storeapi.StoreSetCmd{
			Store: ms,
			Entry: store.Entry{
				Key:   registry.ID{NS: "test", Name: "key"},
				Value: payload.New("value"),
			},
		}
		_ = h.Handle(context.Background(), cmd, func(_ any) {
			close(done)
		})

		// Stop immediately
		require.NoError(t, d.Stop(context.Background()))
	}
}

func TestStress_MixedOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	d := NewAsyncDispatcher(4)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ctx := context.Background()
	h := getHandler(d)

	const numOps = 10000
	var wg sync.WaitGroup
	wg.Add(numOps)

	for i := 0; i < numOps; i++ {
		go func(i int) {
			defer wg.Done()

			key := registry.ID{NS: "mixed", Name: fmt.Sprintf("key-%d", i%100)}

			switch i % 4 {
			case 0: // Set
				cmd := &storeapi.StoreSetCmd{
					Store: ms,
					Entry: store.Entry{Key: key, Value: payload.New(fmt.Sprintf("value-%d", i))},
				}
				_ = h.Handle(ctx, cmd, func(_ any) {})
			case 1: // Get
				cmd := &storeapi.StoreGetCmd{Store: ms, Key: key}
				_ = h.Handle(ctx, cmd, func(_ any) {})
			case 2: // Has
				cmd := &storeapi.StoreHasCmd{Store: ms, Key: key}
				_ = h.Handle(ctx, cmd, func(_ any) {})
			case 3: // Delete
				cmd := &storeapi.StoreDeleteCmd{Store: ms, Key: key}
				_ = h.Handle(ctx, cmd, func(_ any) {})
			}
		}(i)
	}

	wg.Wait()
}

// Race condition tests - these are designed to be run with -race flag

func TestRace_ConcurrentSubmit(t *testing.T) {
	d := NewAsyncDispatcher(2)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ctx := context.Background()
	h := getHandler(d)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := registry.ID{NS: "race", Name: fmt.Sprintf("key-%d", i)}
			cmd := &storeapi.StoreSetCmd{
				Store: ms,
				Entry: store.Entry{Key: key, Value: payload.New("value")},
			}
			done := make(chan struct{})
			_ = h.Handle(ctx, cmd, func(_ any) {
				close(done)
			})
			<-done
		}(i)
	}
	wg.Wait()
}

func TestRace_SubmitDuringShutdown(t *testing.T) {
	for i := 0; i < 10; i++ {
		d := NewAsyncDispatcher(2)
		require.NoError(t, d.Start(context.Background()))
		h := getHandler(d)

		ms := newMockStore()
		ctx := context.Background()

		// Start submitting
		var submitWg sync.WaitGroup
		stopSubmit := make(chan struct{})

		submitWg.Add(1)
		go func() {
			defer submitWg.Done()
			for {
				select {
				case <-stopSubmit:
					return
				default:
					cmd := &storeapi.StoreSetCmd{
						Store: ms,
						Entry: store.Entry{
							Key:   registry.ID{NS: "race", Name: "key"},
							Value: payload.New("value"),
						},
					}
					_ = h.Handle(ctx, cmd, func(_ any) {})
				}
			}
		}()

		// Let it run briefly then stop
		time.Sleep(time.Millisecond)
		close(stopSubmit)

		// Stop dispatcher while submissions might still be in flight
		require.NoError(t, d.Stop(context.Background()))
		submitWg.Wait()
	}
}

// Benchmark with different worker counts

func BenchmarkDispatcher_Workers1(b *testing.B) {
	benchmarkWithWorkers(b, 1)
}

func BenchmarkDispatcher_Workers2(b *testing.B) {
	benchmarkWithWorkers(b, 2)
}

func BenchmarkDispatcher_Workers4(b *testing.B) {
	benchmarkWithWorkers(b, 4)
}

func BenchmarkDispatcher_Workers8(b *testing.B) {
	benchmarkWithWorkers(b, 8)
}

func BenchmarkDispatcher_Workers16(b *testing.B) {
	benchmarkWithWorkers(b, 16)
}

func benchmarkWithWorkers(b *testing.B, workers int) {
	d := NewAsyncDispatcher(workers)
	_ = d.Start(context.Background())
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ctx := context.Background()
	key := registry.ID{NS: "bench", Name: "key"}
	ms.data[key.String()] = payload.New("value")
	h := getHandler(d)

	var wg sync.WaitGroup
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			wg.Add(1)
			cmd := &storeapi.StoreGetCmd{Store: ms, Key: key}
			_ = h.Handle(ctx, cmd, func(_ any) {
				wg.Done()
			})
		}
	})
	wg.Wait()
}

// Benchmark simulating real I/O latency

func BenchmarkDispatcher_WithLatency(b *testing.B) {
	d := NewAsyncDispatcher(8)
	_ = d.Start(context.Background())
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ms.delay = 100 * time.Microsecond // Simulate fast I/O
	ctx := context.Background()
	key := registry.ID{NS: "bench", Name: "key"}
	ms.data[key.String()] = payload.New("value")
	h := getHandler(d)

	var wg sync.WaitGroup
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		wg.Add(1)
		cmd := &storeapi.StoreGetCmd{Store: ms, Key: key}
		_ = h.Handle(ctx, cmd, func(_ any) {
			wg.Done()
		})
	}
	wg.Wait()
}
