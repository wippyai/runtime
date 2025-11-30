package resource

import (
	"sync"
	"testing"

	apiresource "github.com/wippyai/runtime/api/resource"
)

func TestTable(t *testing.T) {
	t.Run("insert and get", func(t *testing.T) {
		table := New()
		defer table.Close()

		h := table.Insert(1, "test")
		if h == 0 {
			t.Fatal("expected non-zero handle")
		}

		v, ok := table.Get(h)
		if !ok {
			t.Fatal("expected value")
		}
		if v.(string) != "test" {
			t.Errorf("got %v, want test", v)
		}
	})

	t.Run("get typed", func(t *testing.T) {
		table := New()
		defer table.Close()

		h := table.Insert(42, "typed")

		// Correct type
		v, ok := table.GetTyped(h, 42)
		if !ok {
			t.Fatal("expected value with correct type")
		}
		if v.(string) != "typed" {
			t.Errorf("got %v, want typed", v)
		}

		// Wrong type
		_, ok = table.GetTyped(h, 99)
		if ok {
			t.Error("expected false for wrong type")
		}
	})

	t.Run("remove", func(t *testing.T) {
		table := New()
		defer table.Close()

		h := table.Insert(1, "removeme")
		table.Remove(h)

		_, ok := table.Get(h)
		if ok {
			t.Error("expected nil after remove")
		}
	})

	t.Run("remove calls dropper", func(t *testing.T) {
		table := New()
		defer table.Close()

		stream := &InputStream{StreamID: 1}
		h := table.Insert(uint32(TypeInputStream), stream)

		table.Remove(h)

		if !stream.Closed {
			t.Error("expected Closed=true after remove")
		}
	})

	t.Run("handle reuse via free list", func(t *testing.T) {
		table := New()
		defer table.Close()

		h1 := table.Insert(1, "first")
		table.Remove(h1)

		h2 := table.Insert(1, "second")

		// Should reuse the freed handle
		if h2 != h1 {
			t.Errorf("handle not reused: got %d, want %d", h2, h1)
		}

		v, ok := table.Get(h2)
		if !ok || v.(string) != "second" {
			t.Error("wrong value after reuse")
		}
	})

	t.Run("borrow prevents remove", func(t *testing.T) {
		table := New()
		defer table.Close()

		h := table.Insert(1, "borrowed")
		table.Borrow(h)

		_, ok := table.Remove(h)
		if ok {
			t.Error("remove should fail with outstanding borrow")
		}

		// Return borrow, then remove works
		table.ReturnBorrow(h)
		_, ok = table.Remove(h)
		if !ok {
			t.Error("remove should succeed after borrow returned")
		}
	})

	t.Run("len and each", func(t *testing.T) {
		table := New()
		defer table.Close()

		table.Insert(1, "a")
		table.Insert(2, "b")
		table.Insert(3, "c")

		if table.Len() != 3 {
			t.Errorf("len = %d, want 3", table.Len())
		}

		count := 0
		table.Each(func(h Handle, typeID uint32, v any) bool {
			count++
			return true
		})
		if count != 3 {
			t.Errorf("each count = %d, want 3", count)
		}
	})

	t.Run("clear", func(t *testing.T) {
		table := New()
		defer table.Close()

		stream := &InputStream{StreamID: 1}
		table.Insert(uint32(TypeInputStream), stream)
		table.Insert(2, "other")

		table.Clear()

		if table.Len() != 0 {
			t.Errorf("len after clear = %d, want 0", table.Len())
		}
		if !stream.Closed {
			t.Error("expected dropper called on clear")
		}
	})

	t.Run("close", func(t *testing.T) {
		table := New()

		stream := &InputStream{StreamID: 1}
		table.Insert(uint32(TypeInputStream), stream)

		table.Close()

		if !stream.Closed {
			t.Error("expected dropper called on close")
		}

		// Insert after close returns 0
		h := table.Insert(1, "after close")
		if h != 0 {
			t.Errorf("insert after close = %d, want 0", h)
		}
	})

	t.Run("double close is safe", func(t *testing.T) {
		table := New()
		table.Close()
		err := table.Close()
		if err != nil {
			t.Errorf("double close error: %v", err)
		}
	})

	t.Run("invalid handle returns false", func(t *testing.T) {
		table := New()
		defer table.Close()

		_, ok := table.Get(0)
		if ok {
			t.Error("handle 0 should be invalid")
		}

		_, ok = table.Get(999)
		if ok {
			t.Error("non-existent handle should return false")
		}
	})
}

func TestTypedTable(t *testing.T) {
	table := New()
	defer table.Close()

	streams := apiresource.NewTypedTable[*InputStream](table, uint32(TypeInputStream))

	t.Run("insert and get", func(t *testing.T) {
		stream := &InputStream{StreamID: 42}
		h := streams.Insert(stream)

		got, ok := streams.Get(h)
		if !ok {
			t.Fatal("expected stream")
		}
		if got.StreamID != 42 {
			t.Errorf("streamID = %d, want 42", got.StreamID)
		}
	})

	t.Run("get wrong type returns false", func(t *testing.T) {
		// Insert with different type
		h := table.Insert(999, "not a stream")

		_, ok := streams.Get(h)
		if ok {
			t.Error("expected false for wrong type")
		}
	})

	t.Run("len counts only typed", func(t *testing.T) {
		streams2 := apiresource.NewTypedTable[*InputStream](table, uint32(TypeInputStream))
		table2 := New()
		defer table2.Close()

		streams3 := apiresource.NewTypedTable[*InputStream](table2, uint32(TypeInputStream))
		out := apiresource.NewTypedTable[*OutputStream](table2, uint32(TypeOutputStream))

		streams3.Insert(&InputStream{StreamID: 1})
		streams3.Insert(&InputStream{StreamID: 2})
		out.Insert(&OutputStream{StreamID: 3})

		if streams3.Len() != 2 {
			t.Errorf("streams len = %d, want 2", streams3.Len())
		}
		if out.Len() != 1 {
			t.Errorf("out len = %d, want 1", out.Len())
		}

		_ = streams2 // unused but shows typed tables share base table
	})

	t.Run("remove", func(t *testing.T) {
		table := New()
		defer table.Close()

		streams := apiresource.NewTypedTable[*InputStream](table, uint32(TypeInputStream))
		stream := &InputStream{StreamID: 10}
		h := streams.Insert(stream)

		got, ok := streams.Remove(h)
		if !ok {
			t.Fatal("expected remove to succeed")
		}
		if got.StreamID != 10 {
			t.Error("wrong stream returned")
		}
		if !stream.Closed {
			t.Error("expected dropper called")
		}
	})
}

func TestInstanceResources(t *testing.T) {
	t.Run("creates typed accessors", func(t *testing.T) {
		res := NewInstanceResources()
		defer res.Close()

		if res.InputStreams() == nil {
			t.Error("expected input streams accessor")
		}
		if res.OutputStreams() == nil {
			t.Error("expected output streams accessor")
		}
		if res.Pollables() == nil {
			t.Error("expected pollables accessor")
		}
	})

	t.Run("resources are isolated by type", func(t *testing.T) {
		res := NewInstanceResources()
		defer res.Close()

		h1 := res.InputStreams().Insert(&InputStream{StreamID: 1})
		h2 := res.OutputStreams().Insert(&OutputStream{StreamID: 2})

		// Can get via correct accessor
		_, ok := res.InputStreams().Get(h1)
		if !ok {
			t.Error("expected input stream")
		}

		_, ok = res.OutputStreams().Get(h2)
		if !ok {
			t.Error("expected output stream")
		}

		// Cross-type access fails
		_, ok = res.InputStreams().Get(h2)
		if ok {
			t.Error("expected false for wrong type")
		}
	})

	t.Run("close releases all", func(t *testing.T) {
		res := NewInstanceResources()

		in := &InputStream{StreamID: 1}
		out := &OutputStream{StreamID: 2}
		res.InputStreams().Insert(in)
		res.OutputStreams().Insert(out)

		res.Close()

		if !in.Closed {
			t.Error("expected input stream closed")
		}
		if !out.Closed {
			t.Error("expected output stream closed")
		}
		if res.Len() != 0 {
			t.Errorf("len after close = %d, want 0", res.Len())
		}
	})
}

func TestConcurrency(t *testing.T) {
	table := New()
	defer table.Close()

	var wg sync.WaitGroup
	workers := 10
	opsPerWorker := 100

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				h := table.Insert(1, i)
				table.Get(h)
				table.Remove(h)
			}
		}()
	}

	wg.Wait()

	// All resources should be cleaned up
	if table.Len() != 0 {
		t.Errorf("len after concurrent ops = %d, want 0", table.Len())
	}
}

func BenchmarkTableInsert(b *testing.B) {
	table := New()
	defer table.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := table.Insert(1, i)
		table.Remove(h)
	}
}

func BenchmarkTableGet(b *testing.B) {
	table := New()
	defer table.Close()

	h := table.Insert(1, "value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		table.Get(h)
	}
}

func BenchmarkTableGetTyped(b *testing.B) {
	table := New()
	defer table.Close()

	h := table.Insert(42, "value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		table.GetTyped(h, 42)
	}
}

func BenchmarkTypedTableInsertGet(b *testing.B) {
	table := New()
	defer table.Close()

	streams := apiresource.NewTypedTable[*InputStream](table, uint32(TypeInputStream))
	stream := &InputStream{StreamID: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := streams.Insert(stream)
		streams.Get(h)
		table.Remove(h)
	}
}

func BenchmarkInstanceResources(b *testing.B) {
	res := NewInstanceResources()
	defer res.Close()

	stream := &InputStream{StreamID: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := res.InputStreams().Insert(stream)
		res.InputStreams().Get(h)
		res.Table().Remove(h)
	}
}
