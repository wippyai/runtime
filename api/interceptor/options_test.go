package interceptor

import (
	"testing"
	"time"
)

func TestNewBag(t *testing.T) {
	bag := NewBag()
	if bag == nil {
		t.Fatal("NewBag returned nil")
	}
	if bag.data == nil {
		t.Fatal("bag.data is nil")
	}
}

func TestNewBagFrom(t *testing.T) {
	data := map[string]any{
		"key1": "value1",
		"key2": 42,
	}
	bag := NewBagFrom(data)

	if val, ok := bag.Get("key1"); !ok || val != "value1" {
		t.Errorf("expected key1=value1, got %v", val)
	}
	if val, ok := bag.Get("key2"); !ok || val != 42 {
		t.Errorf("expected key2=42, got %v", val)
	}

	// Verify it's a copy, not reference
	data["key1"] = "modified"
	if val, _ := bag.Get("key1"); val == "modified" {
		t.Error("bag data should be copied, not referenced")
	}
}

func TestBagSetGet(t *testing.T) {
	bag := NewBag()

	bag.Set("string", "test")
	bag.Set("int", 123)
	bag.Set("bool", true)
	bag.Set("duration", 5*time.Second)

	if val, ok := bag.Get("string"); !ok || val != "test" {
		t.Errorf("expected string=test, got %v", val)
	}
	if val, ok := bag.Get("int"); !ok || val != 123 {
		t.Errorf("expected int=123, got %v", val)
	}
	if val, ok := bag.Get("bool"); !ok || val != true {
		t.Errorf("expected bool=true, got %v", val)
	}
	if val, ok := bag.Get("duration"); !ok || val != 5*time.Second {
		t.Errorf("expected duration=5s, got %v", val)
	}

	if _, ok := bag.Get("nonexistent"); ok {
		t.Error("expected nonexistent key to return false")
	}
}

func TestBagGetString(t *testing.T) {
	bag := NewBag()
	bag.Set("str", "hello")
	bag.Set("notstr", 123)

	if val := bag.GetString("str", "default"); val != "hello" {
		t.Errorf("expected hello, got %s", val)
	}
	if val := bag.GetString("notstr", "default"); val != "default" {
		t.Errorf("expected default for wrong type, got %s", val)
	}
	if val := bag.GetString("missing", "default"); val != "default" {
		t.Errorf("expected default for missing key, got %s", val)
	}
}

func TestBagGetInt(t *testing.T) {
	bag := NewBag()
	bag.Set("num", 42)
	bag.Set("notnum", "string")

	if val := bag.GetInt("num", 0); val != 42 {
		t.Errorf("expected 42, got %d", val)
	}
	if val := bag.GetInt("notnum", 99); val != 99 {
		t.Errorf("expected default 99 for wrong type, got %d", val)
	}
	if val := bag.GetInt("missing", 99); val != 99 {
		t.Errorf("expected default 99 for missing key, got %d", val)
	}
}

func TestBagGetBool(t *testing.T) {
	bag := NewBag()
	bag.Set("flag", true)
	bag.Set("notbool", "string")

	if val := bag.GetBool("flag", false); val != true {
		t.Errorf("expected true, got %v", val)
	}
	if val := bag.GetBool("notbool", false); val != false {
		t.Errorf("expected default false for wrong type, got %v", val)
	}
	if val := bag.GetBool("missing", true); val != true {
		t.Errorf("expected default true for missing key, got %v", val)
	}
}

func TestBagGetDuration(t *testing.T) {
	bag := NewBag()
	bag.Set("timeout", 10*time.Second)
	bag.Set("notduration", "string")

	if val := bag.GetDuration("timeout", 0); val != 10*time.Second {
		t.Errorf("expected 10s, got %v", val)
	}
	if val := bag.GetDuration("notduration", 5*time.Second); val != 5*time.Second {
		t.Errorf("expected default 5s for wrong type, got %v", val)
	}
	if val := bag.GetDuration("missing", 5*time.Second); val != 5*time.Second {
		t.Errorf("expected default 5s for missing key, got %v", val)
	}
}

func TestBagMerge(t *testing.T) {
	bag1 := NewBag()
	bag1.Set("key1", "value1")
	bag1.Set("key2", "original")

	bag2 := NewBag()
	bag2.Set("key2", "overridden")
	bag2.Set("key3", "value3")

	result := bag1.Merge(bag2)

	if val := result.GetString("key1", ""); val != "value1" {
		t.Errorf("expected key1=value1, got %s", val)
	}
	if val := result.GetString("key2", ""); val != "overridden" {
		t.Errorf("expected key2=overridden, got %s", val)
	}
	if val := result.GetString("key3", ""); val != "value3" {
		t.Errorf("expected key3=value3, got %s", val)
	}

	// Verify original bags are unchanged
	if val := bag1.GetString("key2", ""); val != "original" {
		t.Error("merge should not modify original bag1")
	}
	if val := bag1.GetString("key3", ""); val != "" {
		t.Error("merge should not modify original bag1")
	}
}

func TestBagClone(t *testing.T) {
	bag := NewBag()
	bag.Set("key1", "value1")
	bag.Set("key2", 42)

	clone := bag.Clone()

	if val := clone.GetString("key1", ""); val != "value1" {
		t.Errorf("clone missing key1, got %s", val)
	}
	if val := clone.GetInt("key2", 0); val != 42 {
		t.Errorf("clone missing key2, got %d", val)
	}

	// Modify clone and verify original unchanged
	if cloneBag, ok := clone.(*Bag); ok {
		cloneBag.Set("key1", "modified")
		if val := bag.GetString("key1", ""); val == "modified" {
			t.Error("modifying clone should not affect original")
		}
	}
}
