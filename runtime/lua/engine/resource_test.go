package engine

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestValueStore_Get(t *testing.T) {
	store := newValueStore()

	// Test getting non-existent key
	if _, exists := store.Get("nonexistent"); exists {
		t.Errorf("expected non-existent key to return false, got true")
	}

	// Test getting existing key
	store.Set("key1", "value1")
	if val, exists := store.Get("key1"); !exists {
		t.Errorf("expected key to exist")
	} else if val != "value1" {
		t.Errorf("expected value 'value1', got %v", val)
	}
}

func TestValueStore_Set(t *testing.T) {
	store := newValueStore()

	// Test setting value
	store.Set("key1", "value1")
	if val, exists := store.Get("key1"); !exists {
		t.Errorf("expected key to exist after Set")
	} else if val != "value1" {
		t.Errorf("expected value 'value1', got %v", val)
	}

	// Test overwriting value
	store.Set("key1", "newvalue")
	if val, exists := store.Get("key1"); !exists {
		t.Errorf("expected key to exist after overwrite")
	} else if val != "newvalue" {
		t.Errorf("expected value 'newvalue', got %v", val)
	}
}

func TestValueStore_Delete(t *testing.T) {
	store := newValueStore()

	// Test deleting non-existent key
	store.Delete("nonexistent")

	// Test deleting existing key
	store.Set("key1", "value1")
	store.Delete("key1")
	if _, exists := store.Get("key1"); exists {
		t.Errorf("expected key to not exist after Delete")
	}
}

func TestValueStore_GetOrStore(t *testing.T) {
	store := newValueStore()

	// Test with new key
	if val, loaded := store.GetOrStore("key1", "value1"); loaded {
		t.Errorf("expected loaded to be false for new key")
	} else if val != "value1" {
		t.Errorf("expected value 'value1', got %v", val)
	}

	// Test with existing key
	if val, loaded := store.GetOrStore("key1", "different"); !loaded {
		t.Errorf("expected loaded to be true for existing key")
	} else if val != "value1" {
		t.Errorf("expected existing value 'value1', got %v", val)
	}
}

func TestValueStore_CompareAndSwap(t *testing.T) {
	store := newValueStore()

	// Test with non-existent key
	if store.CompareAndSwap("key1", "old", "new") {
		t.Errorf("expected CompareAndSwap to fail for non-existent key")
	}

	// Test with correct old value
	store.Set("key1", "old")
	if !store.CompareAndSwap("key1", "old", "new") {
		t.Errorf("expected CompareAndSwap to succeed")
	}
	if val, _ := store.Get("key1"); val != "new" {
		t.Errorf("expected value to be updated to 'new', got %v", val)
	}

	// Test with wrong old value
	if store.CompareAndSwap("key1", "wrong", "another") {
		t.Errorf("expected CompareAndSwap to fail with wrong old value")
	}
	if val, _ := store.Get("key1"); val != "new" {
		t.Errorf("expected value to remain unchanged, got %v", val)
	}
}

func TestValueStore_Reset(t *testing.T) {
	store := newValueStore()

	// Add some values
	store.Set("key1", "value1")
	store.Set("key2", "value2")

	// Reset
	store.reset()

	// Verify all values are gone
	if _, exists := store.Get("key1"); exists {
		t.Errorf("expected key1 to not exist after reset")
	}
	if _, exists := store.Get("key2"); exists {
		t.Errorf("expected key2 to not exist after reset")
	}
}

func TestResourceManager_AddCleanup(t *testing.T) {
	manager := newResourceManager()

	// Test adding cleanup function
	called := false
	cancel := manager.AddCleanup(func() error {
		called = true
		return nil
	})

	if cancel == nil {
		t.Errorf("expected non-nil cancel function")
	}

	// Test cancel function
	cancel()
	if !called {
		t.Errorf("expected cleanup function to be called")
	}
}

func TestResourceManager_AddCleanupWhenClosed(t *testing.T) {
	manager := newResourceManager()

	// Close the manager
	err := manager.Close()
	if err != nil {
		t.Errorf("expected no error from Close, got %v", err)
	}

	// Try to add cleanup after close
	called := false
	cancel := manager.AddCleanup(func() error {
		called = true
		return nil
	})

	if !called {
		t.Errorf("expected cleanup function to be called immediately when closed")
	}

	// Cancel should be no-op
	cancel()
}

func TestResourceManager_Close(t *testing.T) {
	manager := newResourceManager()

	// Add multiple cleanup functions
	order := make([]int, 0)
	manager.AddCleanup(func() error {
		order = append(order, 1)
		return nil
	})
	manager.AddCleanup(func() error {
		order = append(order, 2)
		return nil
	})
	manager.AddCleanup(func() error {
		order = append(order, 3)
		return nil
	})

	// Close should execute in LIFO order
	err := manager.Close()
	if err != nil {
		t.Errorf("expected no error from Close, got %v", err)
	}

	// Verify LIFO order
	expected := []int{3, 2, 1}
	if len(order) != len(expected) {
		t.Errorf("expected %d cleanup calls, got %d", len(expected), len(order))
	} else {
		for i, val := range order {
			if val != expected[i] {
				t.Errorf("expected cleanup order %v, got %v", expected, order)
				break
			}
		}
	}
}

func TestResourceManager_CloseWithError(t *testing.T) {
	manager := newResourceManager()

	// Add cleanup functions with errors
	manager.AddCleanup(func() error {
		return errors.New("first error")
	})
	manager.AddCleanup(func() error {
		return errors.New("second error")
	})
	manager.AddCleanup(func() error {
		return nil
	})

	// Close should return first error encountered (LIFO order, so "second error" is executed first)
	err := manager.Close()
	if err == nil {
		t.Errorf("expected error from Close")
	} else if err.Error() != "second error" {
		t.Errorf("expected 'second error', got %v", err)
	}
}

func TestResourceManager_CloseMultipleTimes(t *testing.T) {
	manager := newResourceManager()

	// Close first time
	err1 := manager.Close()
	if err1 != nil {
		t.Errorf("expected no error from first Close, got %v", err1)
	}

	// Close second time should return same error
	err2 := manager.Close()
	if !errors.Is(err2, err1) {
		t.Errorf("expected same error from second Close")
	}
}

func TestResourceManager_ConcurrentClose(t *testing.T) {
	manager := newResourceManager()

	// Add cleanup function
	manager.AddCleanup(func() error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	// Close from multiple goroutines
	var wg sync.WaitGroup
	results := make([]error, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index] = manager.Close()
		}(i)
	}

	wg.Wait()

	// All should return the same result
	for i := 1; i < len(results); i++ {
		if !errors.Is(results[i], results[0]) {
			t.Errorf("expected all Close calls to return same result")
		}
	}
}

func TestResourceManager_SetTerminationError(t *testing.T) {
	manager := newResourceManager()

	// Set termination error before any close
	terminationErr := errors.New("termination error")
	manager.setTerminationError(terminationErr)

	// Add a cleanup function that returns an error
	cleanupErr := errors.New("cleanup error")
	_ = manager.AddCleanup(func() error {
		return cleanupErr
	})

	// Close should return either the termination error or the cleanup error, depending on which is set first
	err := manager.Close()
	if !errors.Is(err, terminationErr) && !errors.Is(err, cleanupErr) {
		t.Errorf("expected termination error or cleanup error, got %v", err)
	}
}

func TestResourceManager_Reset(t *testing.T) {
	manager := newResourceManager()

	// Add cleanup and close
	manager.AddCleanup(func() error { return nil })
	err := manager.Close()
	if err != nil {
		t.Errorf("expected no error from Close, got %v", err)
	}

	// Reset
	manager.reset()

	// Should be able to add cleanup again
	cancel := manager.AddCleanup(func() error { return nil })
	if cancel == nil {
		t.Errorf("expected non-nil cancel function after reset")
	}

	// Should be able to close again
	err = manager.Close()
	if err != nil {
		t.Errorf("expected no error from Close after reset, got %v", err)
	}
}
