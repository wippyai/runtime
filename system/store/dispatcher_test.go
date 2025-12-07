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

// testReceiver implements process.ResultReceiver for tests.
type testReceiver struct {
	onComplete func(tag uint64, data any, err error)
}

func (r *testReceiver) CompleteYield(tag uint64, data any, err error) {
	if r.onComplete != nil {
		r.onComplete(tag, data, err)
	}
}

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

func TestDispatcher(t *testing.T) {
	d := NewDispatcher(2)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ctx := context.Background()

	// Test Set
	setDone := make(chan storeapi.StoreSetResponse, 1)
	setCmd := &storeapi.StoreSetCmd{
		Store: ms,
		Entry: store.Entry{
			Key:   registry.NewID("test", "key1"),
			Value: payload.New("value1"),
		},
	}

	err := d.handle(ctx, setCmd, 1, &testReceiver{onComplete: func(_ uint64, data any, _ error) {
		setDone <- data.(storeapi.StoreSetResponse)
	}})
	require.NoError(t, err)

	select {
	case resp := <-setDone:
		assert.NoError(t, resp.Error)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for set response")
	}

	// Test Get
	getDone := make(chan storeapi.StoreGetResponse, 1)
	getCmd := &storeapi.StoreGetCmd{
		Store: ms,
		Key:   registry.NewID("test", "key1"),
	}

	err = d.handle(ctx, getCmd, 2, &testReceiver{onComplete: func(_ uint64, data any, _ error) {
		getDone <- data.(storeapi.StoreGetResponse)
	}})
	require.NoError(t, err)

	select {
	case resp := <-getDone:
		assert.NoError(t, resp.Error)
		assert.NotNil(t, resp.Value)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for get response")
	}

	// Test Has
	hasDone := make(chan storeapi.StoreHasResponse, 1)
	hasCmd := &storeapi.StoreHasCmd{
		Store: ms,
		Key:   registry.NewID("test", "key1"),
	}

	err = d.handle(ctx, hasCmd, 3, &testReceiver{onComplete: func(_ uint64, data any, _ error) {
		hasDone <- data.(storeapi.StoreHasResponse)
	}})
	require.NoError(t, err)

	select {
	case resp := <-hasDone:
		assert.NoError(t, resp.Error)
		assert.True(t, resp.Exists)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for has response")
	}

	// Test Delete
	delDone := make(chan storeapi.StoreDeleteResponse, 1)
	delCmd := &storeapi.StoreDeleteCmd{
		Store: ms,
		Key:   registry.NewID("test", "key1"),
	}

	err = d.handle(ctx, delCmd, 4, &testReceiver{onComplete: func(_ uint64, data any, _ error) {
		delDone <- data.(storeapi.StoreDeleteResponse)
	}})
	require.NoError(t, err)

	select {
	case resp := <-delDone:
		assert.NoError(t, resp.Error)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for delete response")
	}
}

func TestDispatcher_Concurrent(t *testing.T) {
	d := NewDispatcher(4)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ms.delay = 10 * time.Millisecond
	ctx := context.Background()

	var wg sync.WaitGroup
	results := make(chan storeapi.StoreSetResponse, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(tag uint64) {
			defer wg.Done()
			cmd := &storeapi.StoreSetCmd{
				Store: ms,
				Entry: store.Entry{
					Key:   registry.NewID("test", "key"),
					Value: payload.New("value"),
				},
			}
			_ = d.handle(ctx, cmd, tag, &testReceiver{onComplete: func(_ uint64, data any, _ error) {
				results <- data.(storeapi.StoreSetResponse)
			}})
		}(uint64(i))
	}

	wg.Wait()

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
	d := NewDispatcher(4)

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
	t.Run("default workers", func(t *testing.T) {
		d := NewDispatcher(0)
		assert.Equal(t, 4, d.workers)
	})

	t.Run("custom workers", func(t *testing.T) {
		d := NewDispatcher(8)
		assert.Equal(t, 8, d.workers)

		require.NoError(t, d.Start(context.Background()))
		assert.NotNil(t, d.jobs)

		require.NoError(t, d.Stop(context.Background()))
	})
}

func TestDispatcher_ErrorHandling(t *testing.T) {
	d := NewDispatcher(2)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ctx := context.Background()

	// Test Get on non-existent key
	getDone := make(chan storeapi.StoreGetResponse, 1)
	getCmd := &storeapi.StoreGetCmd{
		Store: ms,
		Key:   registry.NewID("test", "nonexistent"),
	}

	err := d.handle(ctx, getCmd, 1, &testReceiver{onComplete: func(_ uint64, data any, _ error) {
		getDone <- data.(storeapi.StoreGetResponse)
	}})
	require.NoError(t, err)

	select {
	case resp := <-getDone:
		assert.ErrorIs(t, resp.Error, store.ErrKeyNotFound)
		assert.Nil(t, resp.Value)
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}

	// Test Has on non-existent key
	hasDone := make(chan storeapi.StoreHasResponse, 1)
	hasCmd := &storeapi.StoreHasCmd{
		Store: ms,
		Key:   registry.NewID("test", "nonexistent"),
	}

	err = d.handle(ctx, hasCmd, 2, &testReceiver{onComplete: func(_ uint64, data any, _ error) {
		hasDone <- data.(storeapi.StoreHasResponse)
	}})
	require.NoError(t, err)

	select {
	case resp := <-hasDone:
		assert.NoError(t, resp.Error)
		assert.False(t, resp.Exists)
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatcher_GracefulShutdown(t *testing.T) {
	d := NewDispatcher(2)
	require.NoError(t, d.Start(context.Background()))

	ms := newMockStore()
	ms.delay = 50 * time.Millisecond
	ctx := context.Background()

	done := make(chan struct{})
	cmd := &storeapi.StoreSetCmd{
		Store: ms,
		Entry: store.Entry{
			Key:   registry.NewID("test", "key"),
			Value: payload.New("value"),
		},
	}
	_ = d.handle(ctx, cmd, 1, &testReceiver{onComplete: func(_ uint64, _ any, _ error) {
		close(done)
	}})

	require.NoError(t, d.Stop(context.Background()))

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
	}
}

func TestDispatcher_AllOperations(t *testing.T) {
	d := NewDispatcher(2)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ctx := context.Background()

	t.Run("Set", func(t *testing.T) {
		done := make(chan storeapi.StoreSetResponse, 1)
		cmd := &storeapi.StoreSetCmd{
			Store: ms,
			Entry: store.Entry{
				Key:   registry.NewID("test", "async-key"),
				Value: payload.New("async-value"),
			},
		}
		require.NoError(t, d.handle(ctx, cmd, 1, &testReceiver{onComplete: func(_ uint64, data any, _ error) {
			done <- data.(storeapi.StoreSetResponse)
		}}))

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
			Key:   registry.NewID("test", "async-key"),
		}
		require.NoError(t, d.handle(ctx, cmd, 2, &testReceiver{onComplete: func(_ uint64, data any, _ error) {
			done <- data.(storeapi.StoreGetResponse)
		}}))

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
			Key:   registry.NewID("test", "async-key"),
		}
		require.NoError(t, d.handle(ctx, cmd, 3, &testReceiver{onComplete: func(_ uint64, data any, _ error) {
			done <- data.(storeapi.StoreHasResponse)
		}}))

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
			Key:   registry.NewID("test", "async-key"),
		}
		require.NoError(t, d.handle(ctx, cmd, 4, &testReceiver{onComplete: func(_ uint64, data any, _ error) {
			done <- data.(storeapi.StoreDeleteResponse)
		}}))

		select {
		case resp := <-done:
			assert.NoError(t, resp.Error)
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})
}

func BenchmarkDispatcher(b *testing.B) {
	d := NewDispatcher(4)
	_ = d.Start(context.Background())
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ctx := context.Background()
	key := registry.NewID("bench", "key")
	ms.data[key.String()] = payload.New("value")

	var wg sync.WaitGroup
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		var tag uint64
		for pb.Next() {
			wg.Add(1)
			tag++
			cmd := &storeapi.StoreGetCmd{Store: ms, Key: key}
			_ = d.handle(ctx, cmd, tag, &testReceiver{onComplete: func(_ uint64, _ any, _ error) {
				wg.Done()
			}})
		}
	})
	wg.Wait()
}

func TestStress_HighConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	d := NewDispatcher(8)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ctx := context.Background()

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
				tag := uint64(goroutineID*opsPerGoroutine + i)

				setDone := make(chan struct{})
				setCmd := &storeapi.StoreSetCmd{
					Store: ms,
					Entry: store.Entry{Key: key, Value: payload.New("value")},
				}
				if err := d.handle(ctx, setCmd, tag, &testReceiver{onComplete: func(_ uint64, data any, _ error) {
					resp := data.(storeapi.StoreSetResponse)
					if resp.Error != nil {
						errors <- resp.Error
					}
					close(setDone)
				}}); err != nil {
					errors <- err
				}
				<-setDone

				getDone := make(chan struct{})
				getCmd := &storeapi.StoreGetCmd{Store: ms, Key: key}
				if err := d.handle(ctx, getCmd, tag+1, &testReceiver{onComplete: func(_ uint64, data any, _ error) {
					resp := data.(storeapi.StoreGetResponse)
					if resp.Error != nil {
						errors <- resp.Error
					}
					close(getDone)
				}}); err != nil {
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
		d := NewDispatcher(4)
		require.NoError(t, d.Start(context.Background()))

		ms := newMockStore()
		done := make(chan struct{})
		cmd := &storeapi.StoreSetCmd{
			Store: ms,
			Entry: store.Entry{
				Key:   registry.NewID("test", "key"),
				Value: payload.New("value"),
			},
		}
		_ = d.handle(context.Background(), cmd, uint64(i), &testReceiver{onComplete: func(_ uint64, _ any, _ error) {
			close(done)
		}})

		require.NoError(t, d.Stop(context.Background()))
	}
}

func TestStress_MixedOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	d := NewDispatcher(4)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ctx := context.Background()

	const numOps = 10000
	var wg sync.WaitGroup
	wg.Add(numOps)

	for i := 0; i < numOps; i++ {
		go func(i int) {
			defer wg.Done()

			key := registry.ID{NS: "mixed", Name: fmt.Sprintf("key-%d", i%100)}
			tag := uint64(i)
			receiver := &testReceiver{onComplete: func(_ uint64, _ any, _ error) {}}

			switch i % 4 {
			case 0:
				cmd := &storeapi.StoreSetCmd{
					Store: ms,
					Entry: store.Entry{Key: key, Value: payload.New(fmt.Sprintf("value-%d", i))},
				}
				_ = d.handle(ctx, cmd, tag, receiver)
			case 1:
				cmd := &storeapi.StoreGetCmd{Store: ms, Key: key}
				_ = d.handle(ctx, cmd, tag, receiver)
			case 2:
				cmd := &storeapi.StoreHasCmd{Store: ms, Key: key}
				_ = d.handle(ctx, cmd, tag, receiver)
			case 3:
				cmd := &storeapi.StoreDeleteCmd{Store: ms, Key: key}
				_ = d.handle(ctx, cmd, tag, receiver)
			}
		}(i)
	}

	wg.Wait()
}

func TestRace_ConcurrentSubmit(t *testing.T) {
	d := NewDispatcher(2)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ctx := context.Background()

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
			_ = d.handle(ctx, cmd, uint64(i), &testReceiver{onComplete: func(_ uint64, _ any, _ error) {
				close(done)
			}})
			<-done
		}(i)
	}
	wg.Wait()
}

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
	d := NewDispatcher(workers)
	_ = d.Start(context.Background())
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ctx := context.Background()
	key := registry.NewID("bench", "key")
	ms.data[key.String()] = payload.New("value")

	var wg sync.WaitGroup
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		var tag uint64
		for pb.Next() {
			wg.Add(1)
			tag++
			cmd := &storeapi.StoreGetCmd{Store: ms, Key: key}
			_ = d.handle(ctx, cmd, tag, &testReceiver{onComplete: func(_ uint64, _ any, _ error) {
				wg.Done()
			}})
		}
	})
	wg.Wait()
}

func BenchmarkDispatcher_WithLatency(b *testing.B) {
	d := NewDispatcher(8)
	_ = d.Start(context.Background())
	defer func() { _ = d.Stop(context.Background()) }()

	ms := newMockStore()
	ms.delay = 100 * time.Microsecond
	ctx := context.Background()
	key := registry.NewID("bench", "key")
	ms.data[key.String()] = payload.New("value")

	var wg sync.WaitGroup
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		wg.Add(1)
		cmd := &storeapi.StoreGetCmd{Store: ms, Key: key}
		_ = d.handle(ctx, cmd, uint64(i), &testReceiver{onComplete: func(_ uint64, _ any, _ error) {
			wg.Done()
		}})
	}
	wg.Wait()
}
