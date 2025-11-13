// Package context provides application-level context management utilities.
package context

import (
	"context"
	"sync"
	"testing"
)

func TestNewValues(t *testing.T) {
	v := NewValues()
	if v == nil {
		t.Fatal("NewValues() returned nil")
	}
	if v.Len() != 0 {
		t.Errorf("NewValues().Len() = %d, want 0", v.Len())
	}
}

func TestValues_SetAndGet(t *testing.T) {
	v := NewValues()

	// Test with string key
	v.Set("key1", "value1")
	if got := v.Get("key1"); got != "value1" {
		t.Errorf("Get(key1) = %v, want value1", got)
	}

	// Test with *Key
	key := &Key{Name: "test.key"}
	v.Set(key, 42)
	if got := v.Get(key); got != 42 {
		t.Errorf("Get(key) = %v, want 42", got)
	}

	// Test with struct{} key
	type customKey struct{}
	v.Set(customKey{}, "custom")
	if got := v.Get(customKey{}); got != "custom" {
		t.Errorf("Get(customKey{}) = %v, want custom", got)
	}

	// Test with int key
	v.Set(123, "int_key")
	if got := v.Get(123); got != "int_key" {
		t.Errorf("Get(123) = %v, want int_key", got)
	}

	// Test non-existent key
	if got := v.Get("nonexistent"); got != nil {
		t.Errorf("Get(nonexistent) = %v, want nil", got)
	}
}

func TestValues_Overwrite(t *testing.T) {
	v := NewValues()

	v.Set("key", "value1")
	if got := v.Get("key"); got != "value1" {
		t.Errorf("Get(key) = %v, want value1", got)
	}

	v.Set("key", "value2")
	if got := v.Get("key"); got != "value2" {
		t.Errorf("Get(key) after overwrite = %v, want value2", got)
	}
}

func TestValues_Len(t *testing.T) {
	v := NewValues()

	if v.Len() != 0 {
		t.Errorf("empty Values.Len() = %d, want 0", v.Len())
	}

	v.Set("key1", "value1")
	if v.Len() != 1 {
		t.Errorf("Values.Len() = %d, want 1", v.Len())
	}

	v.Set("key2", "value2")
	if v.Len() != 2 {
		t.Errorf("Values.Len() = %d, want 2", v.Len())
	}

	// Overwrite doesn't increase length
	v.Set("key1", "new_value")
	if v.Len() != 2 {
		t.Errorf("Values.Len() after overwrite = %d, want 2", v.Len())
	}
}

func TestValues_Iterate(t *testing.T) {
	v := NewValues()

	expected := map[string]any{
		"key1": "value1",
		"key2": 42,
		"key3": true,
	}

	for k, val := range expected {
		v.Set(k, val)
	}

	collected := make(map[string]any)
	v.Iterate(func(key any, value any) {
		if k, ok := key.(string); ok {
			collected[k] = value
		}
	})

	if len(collected) != len(expected) {
		t.Errorf("Iterate() collected %d items, want %d", len(collected), len(expected))
	}

	for k, val := range expected {
		if collected[k] != val {
			t.Errorf("Iterate() key %s = %v, want %v", k, collected[k], val)
		}
	}
}

func TestValues_IterateEmpty(t *testing.T) {
	v := NewValues()

	count := 0
	v.Iterate(func(key any, value any) {
		count++
	})

	if count != 0 {
		t.Errorf("Iterate() on empty Values called fn %d times, want 0", count)
	}
}

func TestValues_Clone(t *testing.T) {
	original := NewValues()
	original.Set("key1", "value1")
	original.Set("key2", 42)
	original.Set("key3", true)

	cloned := original.Clone()
	clone, ok := cloned.(*Values)
	if !ok {
		t.Fatal("Clone() did not return *Values")
	}

	// Verify clone has same values
	if got := clone.Get("key1"); got != "value1" {
		t.Errorf("clone.Get(key1) = %v, want value1", got)
	}
	if got := clone.Get("key2"); got != 42 {
		t.Errorf("clone.Get(key2) = %v, want 42", got)
	}
	if got := clone.Get("key3"); got != true {
		t.Errorf("clone.Get(key3) = %v, want true", got)
	}

	// Verify same length
	if clone.Len() != original.Len() {
		t.Errorf("clone.Len() = %d, want %d", clone.Len(), original.Len())
	}

	// Modify original
	original.Set("key4", "value4")

	// Clone should NOT have new value (independent)
	if got := clone.Get("key4"); got != nil {
		t.Errorf("clone.Get(key4) = %v, want nil (should be independent)", got)
	}

	// Modify clone
	clone.Set("key5", "value5")

	// Original should NOT have clone's new value
	if got := original.Get("key5"); got != nil {
		t.Errorf("original.Get(key5) = %v, want nil (should be independent)", got)
	}
}

func TestValues_CloneEmpty(t *testing.T) {
	original := NewValues()
	cloned := original.Clone()

	if cloned == nil {
		t.Fatal("Clone() of empty Values returned nil")
	}

	clone, ok := cloned.(*Values)
	if !ok {
		t.Fatal("Clone() did not return *Values")
	}

	if clone.Len() != 0 {
		t.Errorf("clone.Len() = %d, want 0", clone.Len())
	}
}

func TestValues_ConcurrentAccess(t *testing.T) {
	v := NewValues()

	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			v.Set(n, n*2)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			val := v.Get(n)
			if val != nil && val != n*2 {
				t.Errorf("Get(%d) = %v, want %d", n, val, n*2)
			}
		}(i)
	}

	// Concurrent Len calls
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = v.Len()
		}()
	}

	// Concurrent Iterate calls
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v.Iterate(func(key any, value any) {
				// Just iterate, don't assert
			})
		}()
	}

	wg.Wait()
}

func TestValues_MultipleTypes(t *testing.T) {
	v := NewValues()

	// Store different types
	v.Set("string", "value")
	v.Set("int", 42)
	v.Set("bool", true)
	v.Set("slice", []int{1, 2, 3})
	v.Set("map", map[string]int{"a": 1})
	v.Set("struct", struct{ Name string }{"test"})

	// Retrieve and verify
	if got := v.Get("string").(string); got != "value" {
		t.Errorf("Get(string) = %v, want value", got)
	}
	if got := v.Get("int").(int); got != 42 {
		t.Errorf("Get(int) = %v, want 42", got)
	}
	if got := v.Get("bool").(bool); got != true {
		t.Errorf("Get(bool) = %v, want true", got)
	}
	if got := v.Get("slice").([]int); len(got) != 3 {
		t.Errorf("Get(slice) length = %v, want 3", len(got))
	}
	if got := v.Get("map").(map[string]int)["a"]; got != 1 {
		t.Errorf("Get(map)[a] = %v, want 1", got)
	}
	if got := v.Get("struct").(struct{ Name string }).Name; got != "test" {
		t.Errorf("Get(struct).Name = %v, want test", got)
	}
}

func TestValues_UsedInFrameContext(t *testing.T) {
	ctx := context.Background()
	_, callCtx := OpenFrameContext(ctx)
	values := NewValues()

	values.Set("user.id", "123")
	values.Set("user.name", "john")

	valuesKey := &Key{Name: "context.values"}
	callCtx.Set(valuesKey, values)

	retrievedVal, ok := callCtx.Get(valuesKey)
	if !ok {
		t.Fatal("retrieved Values not found")
	}
	retrieved := retrievedVal.(*Values)
	if retrieved == nil {
		t.Fatal("retrieved Values is nil")
	}

	if got := retrieved.Get("user.id"); got != "123" {
		t.Errorf("retrieved.Get(user.id) = %v, want 123", got)
	}

	if got := retrieved.Get("user.name"); got != "john" {
		t.Errorf("retrieved.Get(user.name) = %v, want john", got)
	}
}

func TestValuesPair(t *testing.T) {
	values := NewValues()
	values.Set("test", "value")

	pair := ValuesPair(values)
	if pair.Key != ValuesCtx {
		t.Errorf("ValuesPair().Key = %v, want ValuesCtx", pair.Key)
	}
	if pair.Value != values {
		t.Errorf("ValuesPair().Value = %v, want values", pair.Value)
	}
}

func TestSetValues(t *testing.T) {
	ctx, fc := OpenFrameContext(context.Background())
	values := NewValues()
	values.Set("key1", "value1")

	err := SetValues(ctx, values)
	if err != nil {
		t.Errorf("SetValues() error = %v, want nil", err)
	}

	retrieved, ok := fc.Get(ValuesCtx)
	if !ok {
		t.Fatal("SetValues should store values in frame context")
	}

	retrievedValues, ok := retrieved.(*Values)
	if !ok {
		t.Fatal("stored value should be *Values")
	}

	if retrievedValues.Get("key1") != "value1" {
		t.Errorf("retrievedValues.Get(key1) = %v, want value1", retrievedValues.Get("key1"))
	}
}

func TestSetValues_NoFrameContext(t *testing.T) {
	ctx := context.Background()
	values := NewValues()

	err := SetValues(ctx, values)
	if err == nil {
		t.Error("SetValues() without frame context should return error")
	}
}

func TestGetValues(t *testing.T) {
	ctx, fc := OpenFrameContext(context.Background())
	values := NewValues()
	values.Set("key1", "value1")

	fc.Set(ValuesCtx, values)

	retrieved := GetValues(ctx)
	if retrieved == nil {
		t.Fatal("GetValues() should return Values")
	}

	if retrieved.Get("key1") != "value1" {
		t.Errorf("retrieved.Get(key1) = %v, want value1", retrieved.Get("key1"))
	}
}

func TestGetValues_NoFrameContext(t *testing.T) {
	ctx := context.Background()

	retrieved := GetValues(ctx)
	if retrieved != nil {
		t.Error("GetValues() without frame context should return nil")
	}
}

func TestGetValues_NotFound(t *testing.T) {
	ctx, _ := OpenFrameContext(context.Background())

	retrieved := GetValues(ctx)
	if retrieved != nil {
		t.Error("GetValues() when not set should return nil")
	}
}

func TestGetValues_WrongType(t *testing.T) {
	ctx, fc := OpenFrameContext(context.Background())
	fc.Set(ValuesCtx, "not a values")

	retrieved := GetValues(ctx)
	if retrieved != nil {
		t.Error("GetValues() with wrong type should return nil")
	}
}

func TestGetOrCreateValues(t *testing.T) {
	ctx, _ := OpenFrameContext(context.Background())

	values, err := GetOrCreateValues(ctx)
	if err != nil {
		t.Errorf("GetOrCreateValues() error = %v, want nil", err)
	}
	if values == nil {
		t.Fatal("GetOrCreateValues() should create new Values")
	}

	values.Set("key1", "value1")

	values2, err := GetOrCreateValues(ctx)
	if err != nil {
		t.Errorf("GetOrCreateValues() error = %v, want nil", err)
	}
	if values2 != values {
		t.Error("GetOrCreateValues() should return same instance")
	}
	if values2.Get("key1") != "value1" {
		t.Errorf("values2.Get(key1) = %v, want value1", values2.Get("key1"))
	}
}

func TestGetOrCreateValues_NoFrameContext(t *testing.T) {
	ctx := context.Background()

	values, err := GetOrCreateValues(ctx)
	if err == nil {
		t.Error("GetOrCreateValues() without frame context should return error")
	}
	if values != nil {
		t.Error("GetOrCreateValues() without frame context should return nil")
	}
}
