package uow

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestCleanup(t *testing.T) {
	t.Run("basic flow", func(t *testing.T) {
		var closed bool
		_, uw := OnContext(context.Background())
		uw.AddCleanup(func() error {
			closed = true
			return nil
		})

		if err := uw.Close(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !closed {
			t.Error("closer was not called")
		}
	})

	t.Run("from context", func(t *testing.T) {
		ctx, cleanup := OnContext(context.Background())
		got := FromContext(ctx)
		if got != cleanup {
			t.Error("FromContext returned wrong cleanup")
		}

		if FromContext(context.Background()) != nil {
			t.Error("FromContext should return nil for context without cleanup")
		}
	})

	t.Run("multiple closers", func(t *testing.T) {
		var count int
		_, uw := OnContext(context.Background())

		for i := 0; i < 3; i++ {
			uw.AddCleanup(func() error {
				count++
				return nil
			})
		}

		_ = uw.Close()
		if count != 3 {
			t.Errorf("expected 3 closers to be called, got %d", count)
		}
	})

	t.Run("concurrent access", func(t *testing.T) {
		_, uw := OnContext(context.Background())
		var wg sync.WaitGroup

		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				uw.AddCleanup(func() error { return nil })
			}()
		}
		wg.Wait()

		_ = uw.Close()

		if len(uw.closers) != 0 {
			t.Error("closers slice not emptied")
		}
	})

	t.Run("error handling", func(t *testing.T) {
		_, uw := OnContext(context.Background())
		expectedErr := errors.New("first error")

		uw.AddCleanup(func() error { return errors.New("second error") })
		uw.AddCleanup(func() error { return expectedErr })

		err := uw.Close()
		if !errors.Is(err, expectedErr) {
			t.Errorf("expected first error, got: %v", err)
		}
	})

	t.Run("multiple close calls", func(t *testing.T) {
		_, uw := OnContext(context.Background())
		var count int

		uw.AddCleanup(func() error {
			count++
			return nil
		})

		_ = uw.Close()
		_ = uw.Close()

		if count != 1 {
			t.Errorf("closer called %d times, expected once", count)
		}
	})
}
