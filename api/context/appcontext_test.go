// Package context provides application-level context management utilities.
package context

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestNewAppContext(t *testing.T) {
	ac := NewAppContext()
	if ac == nil {
		t.Fatal("NewAppContext() returned nil")
	}
}

func TestAppContext_WithAndGet(t *testing.T) {
	ac := NewAppContext()

	// Test with string key
	ac = ac.With("key1", "value1")
	if got := ac.Get("key1"); got != "value1" {
		t.Errorf("Get(key1) = %v, want value1", got)
	}

	// Test with *Key
	key := &Key{Name: "test.key"}
	ac = ac.With(key, 42)
	if got := ac.Get(key); got != 42 {
		t.Errorf("Get(key) = %v, want 42", got)
	}

	// Test with struct{} key
	type customKey struct{}
	ac = ac.With(customKey{}, "custom")
	if got := ac.Get(customKey{}); got != "custom" {
		t.Errorf("Get(customKey{}) = %v, want custom", got)
	}

	// Test non-existent key
	if got := ac.Get("nonexistent"); got != nil {
		t.Errorf("Get(nonexistent) = %v, want nil", got)
	}
}

func TestAppContext_WriteOnce(t *testing.T) {
	ac := NewAppContext()

	// Set value once
	ac = ac.With("key1", "value1")

	// Should be able to read
	if got := ac.Get("key1"); got != "value1" {
		t.Errorf("Get(key1) = %v, want value1", got)
	}

	// Setting same key again should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("With() for existing key should panic")
		} else if r != "cannot overwrite AppContext key: key already set" {
			t.Errorf("panic message = %v, want 'cannot overwrite AppContext key: key already set'", r)
		}
	}()

	ac.With("key1", "value2")
}

func TestAppContext_ConcurrentReadsAfterSeal(t *testing.T) {
	ac := NewAppContext()

	// Sequential writes during boot (single-threaded)
	for i := 0; i < 100; i++ {
		ac.With(i, i*2)
	}

	// Seal the context
	ac.Seal()

	var wg sync.WaitGroup

	// Concurrent reads after seal
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			val := ac.Get(n)
			if val != n*2 {
				t.Errorf("Get(%d) = %v, want %d", n, val, n*2)
			}
		}(i)
	}

	wg.Wait()
}

func TestWithAppContext(t *testing.T) {
	ac := NewAppContext()
	ac = ac.With("key1", "value1")

	ctx := context.Background()
	ctx = WithAppContext(ctx, ac)

	retrieved := AppFromContext(ctx)
	if retrieved == nil {
		t.Fatal("AppFromContext() returned nil")
	}

	if got := retrieved.Get("key1"); got != "value1" {
		t.Errorf("retrieved.Get(key1) = %v, want value1", got)
	}
}

func TestAppFromContext_NotPresent(t *testing.T) {
	ctx := context.Background()

	retrieved := AppFromContext(ctx)
	if retrieved != nil {
		t.Errorf("AppFromContext() = %v, want nil when not present", retrieved)
	}
}

func TestAppFromContext_WrongType(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, appContextKey, "not an AppContext")

	retrieved := AppFromContext(ctx)
	if retrieved != nil {
		t.Errorf("AppFromContext() = %v, want nil when wrong type", retrieved)
	}
}

func TestAppContext_MultipleTypes(t *testing.T) {
	ac := NewAppContext()

	// Store different types using chaining
	ac = ac.With("string", "value").
		With("int", 42).
		With("bool", true).
		With("slice", []int{1, 2, 3}).
		With("map", map[string]int{"a": 1})

	// Retrieve and verify
	if got := ac.Get("string").(string); got != "value" {
		t.Errorf("Get(string) = %v, want value", got)
	}
	if got := ac.Get("int").(int); got != 42 {
		t.Errorf("Get(int) = %v, want 42", got)
	}
	if got := ac.Get("bool").(bool); got != true {
		t.Errorf("Get(bool) = %v, want true", got)
	}
	if got := ac.Get("slice").([]int); len(got) != 3 {
		t.Errorf("Get(slice) length = %v, want 3", len(got))
	}
	if got := ac.Get("map").(map[string]int)["a"]; got != 1 {
		t.Errorf("Get(map)[a] = %v, want 1", got)
	}
}

func TestNewRootContext(t *testing.T) {
	ctx := NewRootContext()
	if ctx == nil {
		t.Fatal("NewRootContext() returned nil")
	}

	ac := AppFromContext(ctx)
	if ac == nil {
		t.Fatal("NewRootContext() should have AppContext attached")
	}

	ac.With("test.key", "test.value")
	if got := ac.Get("test.key"); got != "test.value" {
		t.Errorf("Get(test.key) = %v, want test.value", got)
	}
}

func TestKey_String(t *testing.T) {
	key := &Key{Name: "test.key", Inherit: true}
	if got := key.String(); got != "test.key" {
		t.Errorf("String() = %v, want test.key", got)
	}
}

func TestNewValues(t *testing.T) {
	values := NewValues()
	if values == nil {
		t.Fatal("NewValues() returned nil")
	}
	values["key"] = "value"
	if got := values["key"]; got != "value" {
		t.Errorf("values[key] = %v, want value", got)
	}
}

func TestValuesPair(t *testing.T) {
	values := NewValues()
	values["test"] = "data"

	pair := ValuesPair(values)
	if pair.Key != ValuesCtx {
		t.Errorf("ValuesPair().Key = %v, want ValuesCtx", pair.Key)
	}
	if pair.Value == nil {
		t.Errorf("ValuesPair().Value = nil, want values")
	}
}

func TestSetValues_NoFrameContext(t *testing.T) {
	ctx := context.Background()
	values := NewValues()

	err := SetValues(ctx, values)
	if err != ErrNoFrameContext {
		t.Errorf("SetValues() error = %v, want ErrNoFrameContext", err)
	}
}

func TestSetValues_WithFrameContext(t *testing.T) {
	ctx := NewRootContext()
	ctx, fc := AcquireFrameContext(ctx)
	defer ReleaseFrameContext(fc)

	values := NewValues()
	values["key"] = "value"

	err := SetValues(ctx, values)
	if err != nil {
		t.Errorf("SetValues() error = %v, want nil", err)
	}

	retrieved := GetValues(ctx)
	if retrieved == nil {
		t.Fatal("GetValues() returned nil")
	}
	if got := retrieved["key"]; got != "value" {
		t.Errorf("retrieved[key] = %v, want value", got)
	}
}

func TestGetValues_NoFrameContext(t *testing.T) {
	ctx := context.Background()
	values := GetValues(ctx)
	if values != nil {
		t.Errorf("GetValues() = %v, want nil", values)
	}
}

func TestGetValues_WrongType(t *testing.T) {
	ctx := NewRootContext()
	ctx, fc := AcquireFrameContext(ctx)
	defer ReleaseFrameContext(fc)

	fc.Set(ValuesCtx, "not values")

	values := GetValues(ctx)
	if values != nil {
		t.Errorf("GetValues() = %v, want nil when wrong type", values)
	}
}

func TestGetOrCreateValues_NoFrameContext(t *testing.T) {
	ctx := context.Background()
	values, err := GetOrCreateValues(ctx)
	if err != ErrNoFrameContext {
		t.Errorf("GetOrCreateValues() error = %v, want ErrNoFrameContext", err)
	}
	if values != nil {
		t.Errorf("GetOrCreateValues() = %v, want nil", values)
	}
}

func TestGetOrCreateValues_Creates(t *testing.T) {
	ctx := NewRootContext()
	ctx, fc := AcquireFrameContext(ctx)
	defer ReleaseFrameContext(fc)

	values, err := GetOrCreateValues(ctx)
	if err != nil {
		t.Errorf("GetOrCreateValues() error = %v, want nil", err)
	}
	if values == nil {
		t.Fatal("GetOrCreateValues() returned nil")
	}

	values["key"] = "value"

	retrieved, err := GetOrCreateValues(ctx)
	if err != nil {
		t.Errorf("GetOrCreateValues() error = %v, want nil", err)
	}
	if got := retrieved["key"]; got != "value" {
		t.Errorf("retrieved[key] = %v, want value", got)
	}
}

func TestGetOrCreateValues_WrongTypeCreatesNew(t *testing.T) {
	ctx := NewRootContext()
	ctx, fc := AcquireFrameContext(ctx)
	defer ReleaseFrameContext(fc)

	fc.Set(ValuesCtx, "not values")

	values, err := GetOrCreateValues(ctx)
	if err != nil {
		t.Errorf("GetOrCreateValues() error = %v, want nil", err)
	}
	if values == nil {
		t.Fatal("GetOrCreateValues() returned nil")
	}
}

func TestErrorInterface(t *testing.T) {
	err := ErrNoFrameContext
	if got := err.Error(); got != "no frame context available" {
		t.Errorf("Error() = %v, want 'no frame context available'", got)
	}
	if got := err.Kind(); got != "Invalid" {
		t.Errorf("Kind() = %v, want Invalid", got)
	}
	if got := err.Retryable(); got.String() != "False" {
		t.Errorf("Retryable() = %v, want False", got)
	}
	if got := err.Details(); got != nil {
		t.Errorf("Details() = %v, want nil", got)
	}
	if got := errors.Unwrap(err); got != nil {
		t.Errorf("Unwrap() = %v, want nil", got)
	}
}

func TestErrNoAppContext(t *testing.T) {
	err := ErrNoAppContext
	if got := err.Error(); got != "no app context available" {
		t.Errorf("Error() = %v, want 'no app context available'", got)
	}
}

func TestErrFrameSealed(t *testing.T) {
	err := ErrFrameSealed
	if got := err.Error(); got != "frame is sealed" {
		t.Errorf("Error() = %v, want 'frame is sealed'", got)
	}
}

func TestNewFrameSealedError(t *testing.T) {
	key := &Key{Name: "test.key"}
	err := NewFrameSealedError(key)
	if got := err.Error(); got != "cannot set key in sealed frame" {
		t.Errorf("Error() = %v, want 'cannot set key in sealed frame'", got)
	}
	if err.Details() == nil {
		t.Fatal("Details() returned nil")
	}
	if val, _ := err.Details().Get("key"); val != key {
		t.Errorf("Details().Get(key) = %v, want key", val)
	}
}
