package attrs

import (
	"testing"
	"time"
)

func TestBag_GetString(t *testing.T) {
	b := NewBagFrom(map[string]any{
		"str":   "value",
		"int":   42,
		"empty": "",
	})

	if got := b.GetString("str", "default"); got != "value" {
		t.Errorf("GetString(str) = %q, want %q", got, "value")
	}

	if got := b.GetString("int", "default"); got != "default" {
		t.Errorf("GetString(int) = %q, want %q", got, "default")
	}

	if got := b.GetString("missing", "default"); got != "default" {
		t.Errorf("GetString(missing) = %q, want %q", got, "default")
	}

	if got := b.GetString("empty", "default"); got != "" {
		t.Errorf("GetString(empty) = %q, want %q", got, "")
	}
}

func TestBag_GetInt(t *testing.T) {
	b := NewBagFrom(map[string]any{
		"int": 42,
		"str": "value",
	})

	if got := b.GetInt("int", 0); got != 42 {
		t.Errorf("GetInt(int) = %d, want %d", got, 42)
	}

	if got := b.GetInt("str", 99); got != 99 {
		t.Errorf("GetInt(str) = %d, want %d", got, 99)
	}

	if got := b.GetInt("missing", 99); got != 99 {
		t.Errorf("GetInt(missing) = %d, want %d", got, 99)
	}
}

func TestBag_GetBool(t *testing.T) {
	b := NewBagFrom(map[string]any{
		"true":  true,
		"false": false,
		"str":   "value",
	})

	if got := b.GetBool("true", false); got != true {
		t.Errorf("GetBool(true) = %v, want %v", got, true)
	}

	if got := b.GetBool("false", true); got != false {
		t.Errorf("GetBool(false) = %v, want %v", got, false)
	}

	if got := b.GetBool("str", true); got != true {
		t.Errorf("GetBool(str) = %v, want %v", got, true)
	}

	if got := b.GetBool("missing", true); got != true {
		t.Errorf("GetBool(missing) = %v, want %v", got, true)
	}
}

func TestBag_GetDuration(t *testing.T) {
	b := NewBagFrom(map[string]any{
		"dur": 5 * time.Second,
		"str": "value",
	})

	if got := b.GetDuration("dur", 0); got != 5*time.Second {
		t.Errorf("GetDuration(dur) = %v, want %v", got, 5*time.Second)
	}

	if got := b.GetDuration("str", 10*time.Second); got != 10*time.Second {
		t.Errorf("GetDuration(str) = %v, want %v", got, 10*time.Second)
	}

	if got := b.GetDuration("missing", 10*time.Second); got != 10*time.Second {
		t.Errorf("GetDuration(missing) = %v, want %v", got, 10*time.Second)
	}
}

func TestBag_GetSlice(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		key  string
		want []string
	}{
		{
			name: "string slice",
			data: map[string]any{"tags": []string{"a", "b", "c"}},
			key:  "tags",
			want: []string{"a", "b", "c"},
		},
		{
			name: "single string",
			data: map[string]any{"tag": "single"},
			key:  "tag",
			want: []string{"single"},
		},
		{
			name: "any slice",
			data: map[string]any{"tags": []any{"a", "b", "c"}},
			key:  "tags",
			want: []string{"a", "b", "c"},
		},
		{
			name: "missing key",
			data: map[string]any{},
			key:  "tags",
			want: nil,
		},
		{
			name: "wrong type",
			data: map[string]any{"tags": 42},
			key:  "tags",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBagFrom(tt.data)
			got := b.GetSlice(tt.key)

			if len(got) != len(tt.want) {
				t.Errorf("GetSlice(%q) len = %d, want %d", tt.key, len(got), len(tt.want))
				return
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("GetSlice(%q)[%d] = %q, want %q", tt.key, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBag_Merge(t *testing.T) {
	b1 := NewBagFrom(map[string]any{
		"a": 1,
		"b": 2,
	})

	b2 := NewBagFrom(map[string]any{
		"b": 20,
		"c": 30,
	})

	merged := b1.Merge(b2)

	if got := merged.GetInt("a", 0); got != 1 {
		t.Errorf("merged.GetInt(a) = %d, want %d", got, 1)
	}

	if got := merged.GetInt("b", 0); got != 20 {
		t.Errorf("merged.GetInt(b) = %d, want %d (should be from b2)", got, 20)
	}

	if got := merged.GetInt("c", 0); got != 30 {
		t.Errorf("merged.GetInt(c) = %d, want %d", got, 30)
	}
}

func TestBag_Clone(t *testing.T) {
	original := NewBagFrom(map[string]any{
		"a": 1,
		"b": "test",
	})

	cloned := original.Clone().(Bag)

	if cloned.GetInt("a", 0) != 1 {
		t.Error("cloned bag should have same values as original")
	}

	cloned.Set("a", 999)

	if original.GetInt("a", 0) != 1 {
		t.Error("modifying clone should not affect original")
	}

	if cloned.GetInt("a", 0) != 999 {
		t.Error("clone should be modifiable independently")
	}
}

func TestBag_Iterate(t *testing.T) {
	b := NewBagFrom(map[string]any{
		"a": 1,
		"b": "test",
		"c": true,
	})

	seen := make(map[string]any)
	b.Iterate(func(key string, value any) {
		seen[key] = value
	})

	if len(seen) != 3 {
		t.Errorf("Iterate() visited %d items, want 3", len(seen))
	}

	if seen["a"] != 1 || seen["b"] != "test" || seen["c"] != true {
		t.Error("Iterate() did not visit all items correctly")
	}
}

func TestBag_Len(t *testing.T) {
	b := NewBag()
	if b.Len() != 0 {
		t.Errorf("empty bag Len() = %d, want 0", b.Len())
	}

	b.Set("a", 1)
	if b.Len() != 1 {
		t.Errorf("bag with 1 item Len() = %d, want 1", b.Len())
	}

	b.Set("b", 2)
	if b.Len() != 2 {
		t.Errorf("bag with 2 items Len() = %d, want 2", b.Len())
	}
}

func TestBag_Keys(t *testing.T) {
	b := NewBagFrom(map[string]any{
		"a": 1,
		"b": 2,
		"c": 3,
	})

	keys := b.Keys()
	if len(keys) != 3 {
		t.Errorf("Keys() returned %d keys, want 3", len(keys))
	}

	keyMap := make(map[string]bool)
	for _, k := range keys {
		keyMap[k] = true
	}

	if !keyMap["a"] || !keyMap["b"] || !keyMap["c"] {
		t.Error("Keys() did not return all keys")
	}
}

func TestBag_GetFloat(t *testing.T) {
	b := NewBagFrom(map[string]any{
		"float": 3.14,
		"int":   42,
		"str":   "value",
		"zero":  0.0,
		"neg":   -1.5,
	})

	if got := b.GetFloat("float", 0); got != 3.14 {
		t.Errorf("GetFloat(float) = %v, want %v", got, 3.14)
	}

	if got := b.GetFloat("int", 99.9); got != 99.9 {
		t.Errorf("GetFloat(int) = %v, want %v", got, 99.9)
	}

	if got := b.GetFloat("str", 99.9); got != 99.9 {
		t.Errorf("GetFloat(str) = %v, want %v", got, 99.9)
	}

	if got := b.GetFloat("missing", 99.9); got != 99.9 {
		t.Errorf("GetFloat(missing) = %v, want %v", got, 99.9)
	}

	if got := b.GetFloat("zero", 99.9); got != 0.0 {
		t.Errorf("GetFloat(zero) = %v, want %v", got, 0.0)
	}

	if got := b.GetFloat("neg", 0); got != -1.5 {
		t.Errorf("GetFloat(neg) = %v, want %v", got, -1.5)
	}
}

func TestBag_GetBag(t *testing.T) {
	innerBag := NewBagFrom(map[string]any{
		"inner_key": "inner_value",
	})

	innerMap := map[string]any{
		"map_key": "map_value",
	}

	b := NewBagFrom(map[string]any{
		"bag":   innerBag,
		"map":   innerMap,
		"str":   "not a bag",
		"attrs": Bag{"attr_key": "attr_value"},
	})

	t.Run("existing bag", func(t *testing.T) {
		got, ok := b.GetBag("bag")
		if !ok {
			t.Error("GetBag(bag) should return true")
		}
		if got.GetString("inner_key", "") != "inner_value" {
			t.Error("GetBag(bag) should return the inner bag")
		}
	})

	t.Run("map to bag", func(t *testing.T) {
		got, ok := b.GetBag("map")
		if !ok {
			t.Error("GetBag(map) should return true")
		}
		if got.GetString("map_key", "") != "map_value" {
			t.Error("GetBag(map) should convert map to bag")
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		_, ok := b.GetBag("str")
		if ok {
			t.Error("GetBag(str) should return false for non-bag values")
		}
	})

	t.Run("missing key", func(t *testing.T) {
		_, ok := b.GetBag("missing")
		if ok {
			t.Error("GetBag(missing) should return false")
		}
	})

	t.Run("Attributes interface", func(t *testing.T) {
		got, ok := b.GetBag("attrs")
		if !ok {
			t.Error("GetBag(attrs) should return true for Attributes")
		}
		if got.GetString("attr_key", "") != "attr_value" {
			t.Error("GetBag(attrs) should work with Attributes interface")
		}
	})
}

func TestBag_NilSafety(t *testing.T) {
	// Bag methods are nil-safe by design (check method implementations)
	// This test verifies the nil-safe behavior doesn't panic
	var b Bag

	t.Run("Get", func(t *testing.T) {
		_, ok := b.Get("key")
		if ok {
			t.Error("nil bag Get() should return false")
		}
	})

	t.Run("GetString", func(t *testing.T) {
		result := b.GetString("key", "default")
		if result != "default" {
			t.Error("nil bag GetString() should return default")
		}
	})

	t.Run("Len", func(t *testing.T) {
		length := b.Len()
		if length != 0 {
			t.Error("nil bag Len() should return 0")
		}
	})

	t.Run("Iterate", func(t *testing.T) {
		called := false
		b.Iterate(func(_ string, _ any) {
			called = true
		})
		if called {
			t.Error("nil bag Iterate() should not call function")
		}
	})
}
