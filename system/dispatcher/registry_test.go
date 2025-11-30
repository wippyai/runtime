package dispatcher

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
)

type mockHandler struct{}

func (h *mockHandler) Handle(_ context.Context, _ dispatcher.Command, _ dispatcher.EmitFunc) error {
	return nil
}

type mockCommand struct {
	id dispatcher.CommandID
}

func (c *mockCommand) CmdID() dispatcher.CommandID { return c.id }

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()
	h := &mockHandler{}

	r.Register(1, h)
	assert.Equal(t, h, r.Get(1))
}

func TestRegistry_RegisterExtended(t *testing.T) {
	r := NewRegistry()
	h := &mockHandler{}

	r.Register(300, h)
	assert.Equal(t, h, r.Get(300))
}

func TestRegistry_RegisterDuplicatePanics(t *testing.T) {
	r := NewRegistry()
	h := &mockHandler{}

	r.Register(1, h)

	assert.Panics(t, func() {
		r.Register(1, h)
	})
}

func TestRegistry_RegisterExtendedDuplicatePanics(t *testing.T) {
	r := NewRegistry()
	h := &mockHandler{}

	r.Register(300, h)

	assert.Panics(t, func() {
		r.Register(300, h)
	})
}

func TestRegistry_RegisterAfterFreezePanics(t *testing.T) {
	r := NewRegistry()
	r.Freeze()

	assert.Panics(t, func() {
		r.Register(1, &mockHandler{})
	})
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := NewRegistry()
	assert.Nil(t, r.Get(1))
	assert.Nil(t, r.Get(300))
}

func TestRegistry_Has(t *testing.T) {
	r := NewRegistry()
	h := &mockHandler{}

	assert.False(t, r.Has(1))
	r.Register(1, h)
	assert.True(t, r.Has(1))
}

func TestRegistry_HasExtended(t *testing.T) {
	r := NewRegistry()
	h := &mockHandler{}

	assert.False(t, r.Has(300))
	r.Register(300, h)
	assert.True(t, r.Has(300))
}

func TestRegistry_Freeze(t *testing.T) {
	r := NewRegistry()
	h := &mockHandler{}

	r.Register(1, h)
	r.Register(300, h)
	r.Freeze()

	// Should still work after freeze
	assert.Equal(t, h, r.Get(1))
	assert.Equal(t, h, r.Get(300))
	assert.True(t, r.Has(1))
	assert.True(t, r.Has(300))
}

func TestRegistry_Dispatch(t *testing.T) {
	r := NewRegistry()
	h := &mockHandler{}

	r.Register(1, h)
	r.Freeze()

	cmd := &mockCommand{id: 1}
	assert.Equal(t, h, r.Dispatch(cmd))
}

func TestRegistry_ConcurrentRegister(t *testing.T) {
	r := NewRegistry()

	var wg sync.WaitGroup
	const numHandlers = 100

	wg.Add(numHandlers)
	for i := range numHandlers {
		go func(id dispatcher.CommandID) {
			defer wg.Done()
			r.Register(id, &mockHandler{})
		}(dispatcher.CommandID(i)) //nolint:gosec // safe test conversion
	}

	wg.Wait()

	for i := range numHandlers {
		require.NotNil(t, r.Get(dispatcher.CommandID(i)), "handler %d should exist", i) //nolint:gosec // safe test conversion
	}
}

func TestRegistry_ConcurrentGet(t *testing.T) {
	r := NewRegistry()
	h := &mockHandler{}

	const numHandlers = 100
	for i := range numHandlers {
		r.Register(dispatcher.CommandID(i), h) //nolint:gosec // safe test conversion
	}
	r.Freeze()

	var wg sync.WaitGroup
	const numGoroutines = 100
	const numIterations = 1000

	wg.Add(numGoroutines)
	for g := 0; g < numGoroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < numIterations; i++ {
				id := dispatcher.CommandID(i % numHandlers) //nolint:gosec // safe test conversion
				got := r.Get(id)
				if got != h {
					t.Errorf("expected handler, got %v", got)
				}
			}
		}()
	}

	wg.Wait()
}

// Benchmarks

func BenchmarkRegistry_Get_Frozen(b *testing.B) {
	r := NewRegistry()
	h := &mockHandler{}
	r.Register(1, h)
	r.Freeze()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Get(1)
	}
}

func BenchmarkRegistry_Get_Frozen_Parallel(b *testing.B) {
	r := NewRegistry()
	h := &mockHandler{}
	r.Register(1, h)
	r.Freeze()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = r.Get(1)
		}
	})
}

func BenchmarkRegistry_Get_Extended_Frozen(b *testing.B) {
	r := NewRegistry()
	h := &mockHandler{}
	r.Register(300, h)
	r.Freeze()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Get(300)
	}
}

func BenchmarkRegistry_Get_NotFrozen(b *testing.B) {
	r := NewRegistry()
	h := &mockHandler{}
	r.Register(1, h)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Get(1)
	}
}

func BenchmarkRegistry_Get_NotFrozen_Parallel(b *testing.B) {
	r := NewRegistry()
	h := &mockHandler{}
	r.Register(1, h)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = r.Get(1)
		}
	})
}

func BenchmarkRegistry_Has_Frozen(b *testing.B) {
	r := NewRegistry()
	h := &mockHandler{}
	r.Register(1, h)
	r.Freeze()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Has(1)
	}
}

func BenchmarkRegistry_Dispatch_Frozen(b *testing.B) {
	r := NewRegistry()
	h := &mockHandler{}
	r.Register(1, h)
	r.Freeze()

	cmd := &mockCommand{id: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Dispatch(cmd)
	}
}
