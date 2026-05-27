// SPDX-License-Identifier: MPL-2.0

package resource

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
)

func TestSetStore(t *testing.T) {
	t.Run("with frame context", func(t *testing.T) {
		ctx, fc := ctxapi.OpenFrameContext(context.Background())
		defer func() { _ = fc.Close() }()

		store := NewStore()
		defer func() { _ = store.Close() }()

		err := SetStore(ctx, store)
		require.NoError(t, err)

		got := GetStore(ctx)
		assert.Equal(t, store, got)
	})

	t.Run("without frame context", func(t *testing.T) {
		ctx := context.Background()
		store := NewStore()
		defer func() { _ = store.Close() }()

		err := SetStore(ctx, store)
		assert.Equal(t, ctxapi.ErrNoFrameContext, err)
	})
}

func TestGetStore(t *testing.T) {
	t.Run("no frame context", func(t *testing.T) {
		ctx := context.Background()
		got := GetStore(ctx)
		assert.Nil(t, got)
	})

	t.Run("frame context without store", func(t *testing.T) {
		ctx, fc := ctxapi.OpenFrameContext(context.Background())
		defer func() { _ = fc.Close() }()

		got := GetStore(ctx)
		assert.Nil(t, got)
	})
}

func TestGetTable(t *testing.T) {
	t.Run("no store", func(t *testing.T) {
		ctx := context.Background()
		got := GetTable(ctx)
		assert.Nil(t, got)
	})

	t.Run("with store", func(t *testing.T) {
		ctx, fc := ctxapi.OpenFrameContext(context.Background())
		defer func() { _ = fc.Close() }()

		store := NewStore()
		defer func() { _ = store.Close() }()
		require.NoError(t, SetStore(ctx, store))

		got := GetTable(ctx)
		assert.NotNil(t, got)
		assert.Equal(t, store.Table(), got)
	})
}

func TestNewStore(t *testing.T) {
	store := NewStore()
	require.NotNil(t, store)
	assert.NotNil(t, store.Table())
	assert.False(t, store.IsClosed())
	_ = store.Close()
}

func TestStore_Table(t *testing.T) {
	store := NewStore()
	defer func() { _ = store.Close() }()

	table := store.Table()
	assert.NotNil(t, table)

	h := table.Insert(1, "test")
	v, ok := table.Get(h)
	assert.True(t, ok)
	assert.Equal(t, "test", v)
}

func TestStore_AddCleanup(t *testing.T) {
	t.Run("cleanup runs on close", func(t *testing.T) {
		store := NewStore()
		var cleanupOrder []int

		_ = store.AddCleanup(func() error {
			cleanupOrder = append(cleanupOrder, 1)
			return nil
		})
		_ = store.AddCleanup(func() error {
			cleanupOrder = append(cleanupOrder, 2)
			return nil
		})
		_ = store.AddCleanup(func() error {
			cleanupOrder = append(cleanupOrder, 3)
			return nil
		})

		err := store.Close()
		assert.NoError(t, err)
		assert.Equal(t, []int{3, 2, 1}, cleanupOrder) // LIFO order
	})

	t.Run("cancel prevents cleanup", func(t *testing.T) {
		store := NewStore()
		var cleanupRan bool

		cancel := store.AddCleanup(func() error {
			cleanupRan = true
			return nil
		})
		cancel()

		err := store.Close()
		assert.NoError(t, err)
		assert.False(t, cleanupRan)
	})

	t.Run("cleanup on closed store returns noop cancel", func(_ *testing.T) {
		store := NewStore()
		_ = store.Close()

		cancel := store.AddCleanup(func() error {
			return nil
		})
		cancel() // Should not panic
	})

	t.Run("cleanup error is returned", func(t *testing.T) {
		store := NewStore()
		expectedErr := errors.New("cleanup failed")

		_ = store.AddCleanup(func() error {
			return expectedErr
		})

		err := store.Close()
		assert.Equal(t, expectedErr, err)
	})

	t.Run("first error is returned", func(t *testing.T) {
		store := NewStore()
		firstErr := errors.New("first error")

		_ = store.AddCleanup(func() error {
			return errors.New("second error")
		})
		_ = store.AddCleanup(func() error {
			return firstErr
		})

		err := store.Close()
		assert.Equal(t, firstErr, err)
	})
}

func TestStore_AddCleanupBounded(t *testing.T) {
	t.Run("cancelled cleanups do not accumulate", func(t *testing.T) {
		store := NewStore()
		defer func() { _ = store.Close() }()

		const n = 100000
		for i := 0; i < n; i++ {
			cancel := store.AddCleanup(func() error { return nil })
			cancel()
			assert.Equal(t, 0, store.liveCleanups(),
				"live cleanup count must stay bounded after cancel")
		}
	})

	t.Run("live count tracks uncancelled cleanups", func(t *testing.T) {
		store := NewStore()
		defer func() { _ = store.Close() }()

		c1 := store.AddCleanup(func() error { return nil })
		c2 := store.AddCleanup(func() error { return nil })
		c3 := store.AddCleanup(func() error { return nil })
		assert.Equal(t, 3, store.liveCleanups())

		c2()
		assert.Equal(t, 2, store.liveCleanups())

		c1()
		c3()
		assert.Equal(t, 0, store.liveCleanups())
	})

	t.Run("interleaved add and cancel stays bounded", func(t *testing.T) {
		store := NewStore()
		defer func() { _ = store.Close() }()

		const n = 100000
		for i := 0; i < n; i++ {
			cancel := store.AddCleanup(func() error { return nil })
			cancel()
		}
		assert.Equal(t, 0, store.liveCleanups())
	})
}

func TestStore_Cancel(t *testing.T) {
	t.Run("cancel is idempotent", func(t *testing.T) {
		store := NewStore()
		var ran int

		cancel := store.AddCleanup(func() error {
			ran++
			return nil
		})
		cancel()
		cancel()
		cancel()

		err := store.Close()
		assert.NoError(t, err)
		assert.Equal(t, 0, ran)
	})

	t.Run("cancel after close is safe", func(t *testing.T) {
		store := NewStore()
		cancel := store.AddCleanup(func() error { return nil })
		_ = store.Close()
		cancel() // no panic, no-op
	})

	t.Run("uncancelled cleanups still run after some cancels", func(t *testing.T) {
		store := NewStore()
		var order []int

		_ = store.AddCleanup(func() error {
			order = append(order, 1)
			return nil
		})
		c2 := store.AddCleanup(func() error {
			order = append(order, 2)
			return nil
		})
		_ = store.AddCleanup(func() error {
			order = append(order, 3)
			return nil
		})

		c2()

		err := store.Close()
		assert.NoError(t, err)
		assert.Equal(t, []int{3, 1}, order) // LIFO, 2 cancelled
	})
}

func TestStore_PooledReuseStartsEmpty(t *testing.T) {
	store := NewStore()
	_ = store.AddCleanup(func() error { return nil })
	_ = store.AddCleanup(func() error { return nil })
	assert.Equal(t, 2, store.liveCleanups())
	_ = store.Close()

	// Reacquire from pool; may or may not be the same instance, but any
	// instance handed out must start with no live cleanups.
	reused := NewStore()
	defer func() { _ = reused.Close() }()
	assert.Equal(t, 0, reused.liveCleanups())
	assert.False(t, reused.IsClosed())
}

func TestStore_Close(t *testing.T) {
	t.Run("close twice is safe", func(t *testing.T) {
		store := NewStore()

		err1 := store.Close()
		err2 := store.Close()

		assert.NoError(t, err1)
		assert.NoError(t, err2)
	})

	t.Run("close resets table", func(_ *testing.T) {
		store := NewStore()
		_ = store.Table().Insert(1, "test")
		_ = store.Close()
		// After close, store is returned to pool
	})
}

func TestStore_IsClosed(t *testing.T) {
	store := NewStore()
	assert.False(t, store.IsClosed())

	_ = store.Close()
	assert.True(t, store.IsClosed())
}

// Table tests

type dropperMock struct {
	dropped bool
}

func (d *dropperMock) Drop() {
	d.dropped = true
}

func TestNewTable(t *testing.T) {
	table := NewTable()
	require.NotNil(t, table)
	assert.Equal(t, 0, table.Len())
	assert.False(t, table.IsClosed())
	_ = table.Close()
}

func TestTable_Insert(t *testing.T) {
	table := NewTable()
	defer func() { _ = table.Close() }()

	h1 := table.Insert(1, "value1")
	h2 := table.Insert(2, "value2")

	assert.NotEqual(t, Handle(0), h1)
	assert.NotEqual(t, Handle(0), h2)
	assert.NotEqual(t, h1, h2)
}

func TestTable_Insert_Closed(t *testing.T) {
	table := NewTable()
	_ = table.Close()

	h := table.Insert(1, "value")
	assert.Equal(t, Handle(0), h)
}

func TestTable_Get(t *testing.T) {
	table := NewTable()
	defer func() { _ = table.Close() }()

	h := table.Insert(1, "test value")

	t.Run("valid handle", func(t *testing.T) {
		v, ok := table.Get(h)
		assert.True(t, ok)
		assert.Equal(t, "test value", v)
	})

	t.Run("zero handle", func(t *testing.T) {
		v, ok := table.Get(0)
		assert.False(t, ok)
		assert.Nil(t, v)
	})

	t.Run("invalid handle", func(t *testing.T) {
		v, ok := table.Get(999)
		assert.False(t, ok)
		assert.Nil(t, v)
	})
}

func TestTable_GetTyped(t *testing.T) {
	table := NewTable()
	defer func() { _ = table.Close() }()

	h := table.Insert(42, "typed value")

	t.Run("matching type", func(t *testing.T) {
		v, ok := table.GetTyped(h, 42)
		assert.True(t, ok)
		assert.Equal(t, "typed value", v)
	})

	t.Run("wrong type", func(t *testing.T) {
		v, ok := table.GetTyped(h, 99)
		assert.False(t, ok)
		assert.Nil(t, v)
	})

	t.Run("zero handle", func(t *testing.T) {
		v, ok := table.GetTyped(0, 42)
		assert.False(t, ok)
		assert.Nil(t, v)
	})

	t.Run("invalid handle", func(t *testing.T) {
		v, ok := table.GetTyped(999, 42)
		assert.False(t, ok)
		assert.Nil(t, v)
	})
}

func TestTable_Remove(t *testing.T) {
	table := NewTable()
	defer func() { _ = table.Close() }()

	t.Run("removes value", func(t *testing.T) {
		h := table.Insert(1, "to remove")

		v, ok := table.Remove(h)
		assert.True(t, ok)
		assert.Equal(t, "to remove", v)

		_, ok = table.Get(h)
		assert.False(t, ok)
	})

	t.Run("calls Drop on Dropper", func(t *testing.T) {
		dropper := &dropperMock{}
		h := table.Insert(1, dropper)

		_, ok := table.Remove(h)
		assert.True(t, ok)
		assert.True(t, dropper.dropped)
	})

	t.Run("zero handle", func(t *testing.T) {
		v, ok := table.Remove(0)
		assert.False(t, ok)
		assert.Nil(t, v)
	})

	t.Run("invalid handle", func(t *testing.T) {
		v, ok := table.Remove(999)
		assert.False(t, ok)
		assert.Nil(t, v)
	})

	t.Run("already removed", func(t *testing.T) {
		h := table.Insert(1, "test")
		_, _ = table.Remove(h)

		v, ok := table.Remove(h)
		assert.False(t, ok)
		assert.Nil(t, v)
	})

	t.Run("cannot remove borrowed", func(t *testing.T) {
		h := table.Insert(1, "borrowed")
		_ = table.Borrow(h)

		v, ok := table.Remove(h)
		assert.False(t, ok)
		assert.Nil(t, v)

		_ = table.ReturnBorrow(h)
		v, ok = table.Remove(h)
		assert.True(t, ok)
		assert.Equal(t, "borrowed", v)
	})
}

func TestTable_Borrow(t *testing.T) {
	table := NewTable()
	defer func() { _ = table.Close() }()

	h := table.Insert(1, "test")

	t.Run("borrow valid handle", func(t *testing.T) {
		ok := table.Borrow(h)
		assert.True(t, ok)
		_ = table.ReturnBorrow(h)
	})

	t.Run("borrow zero handle", func(t *testing.T) {
		ok := table.Borrow(0)
		assert.False(t, ok)
	})

	t.Run("borrow invalid handle", func(t *testing.T) {
		ok := table.Borrow(999)
		assert.False(t, ok)
	})
}

func TestTable_ReturnBorrow(t *testing.T) {
	table := NewTable()
	defer func() { _ = table.Close() }()

	h := table.Insert(1, "test")
	_ = table.Borrow(h)

	t.Run("return valid borrow", func(t *testing.T) {
		ok := table.ReturnBorrow(h)
		assert.True(t, ok)
	})

	t.Run("return zero handle", func(t *testing.T) {
		ok := table.ReturnBorrow(0)
		assert.False(t, ok)
	})

	t.Run("return invalid handle", func(t *testing.T) {
		ok := table.ReturnBorrow(999)
		assert.False(t, ok)
	})

	t.Run("return when no borrows", func(t *testing.T) {
		ok := table.ReturnBorrow(h)
		assert.False(t, ok)
	})
}

func TestTable_TypeID(t *testing.T) {
	table := NewTable()
	defer func() { _ = table.Close() }()

	h := table.Insert(42, "test")

	t.Run("valid handle", func(t *testing.T) {
		typeID, ok := table.TypeID(h)
		assert.True(t, ok)
		assert.Equal(t, uint32(42), typeID)
	})

	t.Run("zero handle", func(t *testing.T) {
		_, ok := table.TypeID(0)
		assert.False(t, ok)
	})

	t.Run("invalid handle", func(t *testing.T) {
		_, ok := table.TypeID(999)
		assert.False(t, ok)
	})

	t.Run("removed handle", func(t *testing.T) {
		h2 := table.Insert(1, "temp")
		_, _ = table.Remove(h2)

		_, ok := table.TypeID(h2)
		assert.False(t, ok)
	})
}

func TestTable_Len(t *testing.T) {
	table := NewTable()
	defer func() { _ = table.Close() }()

	assert.Equal(t, 0, table.Len())

	h1 := table.Insert(1, "a")
	assert.Equal(t, 1, table.Len())

	h2 := table.Insert(1, "b")
	assert.Equal(t, 2, table.Len())

	_, _ = table.Remove(h1)
	assert.Equal(t, 1, table.Len())

	_, _ = table.Remove(h2)
	assert.Equal(t, 0, table.Len())
}

func TestTable_Each(t *testing.T) {
	table := NewTable()
	defer func() { _ = table.Close() }()

	_ = table.Insert(1, "a")
	_ = table.Insert(2, "b")
	_ = table.Insert(3, "c")

	t.Run("iterates all", func(t *testing.T) {
		var values []any
		table.Each(func(_ Handle, _ uint32, v any) bool {
			values = append(values, v)
			return true
		})
		assert.Len(t, values, 3)
	})

	t.Run("stops early", func(t *testing.T) {
		count := 0
		table.Each(func(_ Handle, _ uint32, _ any) bool {
			count++
			return count < 2
		})
		assert.Equal(t, 2, count)
	})
}

func TestTable_Clear(t *testing.T) {
	table := NewTable()
	defer func() { _ = table.Close() }()

	dropper := &dropperMock{}
	_ = table.Insert(1, "a")
	_ = table.Insert(2, dropper)

	table.Clear()

	assert.Equal(t, 0, table.Len())
	assert.True(t, dropper.dropped)
}

func TestTable_Close(t *testing.T) {
	t.Run("calls Drop on all", func(t *testing.T) {
		table := NewTable()
		dropper := &dropperMock{}
		_ = table.Insert(1, dropper)

		err := table.Close()
		assert.NoError(t, err)
		assert.True(t, dropper.dropped)
		assert.True(t, table.IsClosed())
	})

	t.Run("double close is safe", func(t *testing.T) {
		table := NewTable()
		err1 := table.Close()
		err2 := table.Close()

		assert.NoError(t, err1)
		assert.NoError(t, err2)
	})
}

func TestTable_Reset(t *testing.T) {
	table := NewTable()

	dropper := &dropperMock{}
	_ = table.Insert(1, dropper)
	_ = table.Insert(2, "test")

	table.Reset()

	assert.True(t, dropper.dropped)
	assert.Equal(t, 0, table.Len())
	assert.False(t, table.IsClosed())

	// Can use after reset
	h := table.Insert(1, "new")
	v, ok := table.Get(h)
	assert.True(t, ok)
	assert.Equal(t, "new", v)
}

func TestTable_HandleReuse(t *testing.T) {
	table := NewTable()
	defer func() { _ = table.Close() }()

	h1 := table.Insert(1, "first")
	_, _ = table.Remove(h1)

	h2 := table.Insert(1, "second")
	// Handle should be reused from free list
	assert.Equal(t, h1, h2)

	v, ok := table.Get(h2)
	assert.True(t, ok)
	assert.Equal(t, "second", v)
}

// TypedTable tests

func TestTypedTable(t *testing.T) {
	table := NewTable()
	defer func() { _ = table.Close() }()

	typed := NewTypedTable[string](table, 100)

	t.Run("insert and get", func(t *testing.T) {
		h := typed.Insert("typed value")
		v, ok := typed.Get(h)
		assert.True(t, ok)
		assert.Equal(t, "typed value", v)
	})

	t.Run("get wrong type returns false", func(t *testing.T) {
		h := table.Insert(999, "wrong type")
		v, ok := typed.Get(h)
		assert.False(t, ok)
		assert.Equal(t, "", v)
	})

	t.Run("remove", func(t *testing.T) {
		h := typed.Insert("to remove")
		v, ok := typed.Remove(h)
		assert.True(t, ok)
		assert.Equal(t, "to remove", v)

		_, ok = typed.Get(h)
		assert.False(t, ok)
	})

	t.Run("remove wrong type", func(t *testing.T) {
		h := table.Insert(999, "wrong type")
		v, ok := typed.Remove(h)
		assert.False(t, ok)
		assert.Equal(t, "", v)
	})

	t.Run("len counts only typed", func(t *testing.T) {
		// Use fresh table for this test
		t2 := NewTable()
		defer func() { _ = t2.Close() }()
		typed2 := NewTypedTable[string](t2, 100)

		_ = typed2.Insert("a")
		_ = typed2.Insert("b")
		_ = t2.Insert(999, "other type")

		assert.Equal(t, 2, typed2.Len())
	})

	t.Run("each iterates only typed", func(t *testing.T) {
		// Clear first
		table.Clear()

		_ = typed.Insert("x")
		_ = typed.Insert("y")
		_ = table.Insert(999, "other")

		var values []string
		typed.Each(func(_ Handle, v string) bool {
			values = append(values, v)
			return true
		})
		assert.Equal(t, []string{"x", "y"}, values)
	})
}

func TestTable_Concurrent(_ *testing.T) {
	table := NewTable()
	defer func() { _ = table.Close() }()

	var wg sync.WaitGroup
	const goroutines = 10
	const operations = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				if id < 0 || id > 2147483647 {
					return
				}
				h := table.Insert(uint32(id), j)
				_, _ = table.Get(h)
				_, _ = table.TypeID(h)
				_ = table.Len()
				if j%2 == 0 {
					_ = table.Borrow(h)
					_ = table.ReturnBorrow(h)
				}
				_, _ = table.Remove(h)
			}
		}(i)
	}

	wg.Wait()
}

// ReaderProvider interface test
type mockReaderProvider struct{}

func (m *mockReaderProvider) GetReader(_ context.Context) (io.Reader, error) {
	return strings.NewReader("test content"), nil
}

func TestReaderProvider(t *testing.T) {
	var rp ReaderProvider = &mockReaderProvider{}
	r, err := rp.GetReader(context.Background())
	require.NoError(t, err)
	require.NotNil(t, r)
}
