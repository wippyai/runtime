// Package context provides application-level context management utilities.
package context

import (
	"testing"
)

func TestNewContexter(t *testing.T) {
	tests := []struct {
		name string
		want bool // checks if shared map is initialized
	}{
		{
			name: "new string contexter",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewContexter[string]()
			if (ctx.shared != nil) != tt.want {
				t.Errorf("NewContexter() shared map initialization = %v, want %v", ctx.shared != nil, tt.want)
			}
		})
	}
}

func TestContexter_WithValue(t *testing.T) {
	// Test with different types to ensure generic functionality
	t.Run("string type", func(t *testing.T) {
		ctx := NewContexter[string]()
		ctx.SetValue("test", "value")

		if val, ok := ctx.shared["test"]; !ok || val != "value" {
			t.Errorf("SetValue() failed to store string value, got %v, want %v", val, "value")
		}
	})

	t.Run("int type", func(t *testing.T) {
		ctx := NewContexter[int]()
		ctx.SetValue("test", 42)

		if val, ok := ctx.shared["test"]; !ok || val != 42 {
			t.Errorf("SetValue() failed to store int value, got %v, want %v", val, 42)
		}
	})

	t.Run("struct type", func(t *testing.T) {
		type testStruct struct {
			field string
		}
		ctx := NewContexter[testStruct]()
		value := testStruct{field: "test"}
		ctx.SetValue("test", value)

		if val, ok := ctx.shared["test"]; !ok || val.field != "test" {
			t.Errorf("SetValue() failed to store struct value, got %v, want %v", val, value)
		}
	})

	t.Run("overwrite existing value", func(t *testing.T) {
		ctx := NewContexter[string]()
		ctx.SetValue("test", "value1")
		ctx.SetValue("test", "value2")

		if val, ok := ctx.shared["test"]; !ok || val != "value2" {
			t.Errorf("SetValue() failed to overwrite value, got %v, want %v", val, "value2")
		}
	})
}

func TestContexter_Value(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func() *Contexter[string]
		key       string
		wantVal   string
		wantOk    bool
	}{
		{
			name: "existing value",
			setupFunc: func() *Contexter[string] {
				ctx := NewContexter[string]()
				ctx.SetValue("test", "value")
				return ctx
			},
			key:     "test",
			wantVal: "value",
			wantOk:  true,
		},
		{
			name: "non-existent key",
			setupFunc: func() *Contexter[string] {
				return NewContexter[string]()
			},
			key:     "missing",
			wantVal: "",
			wantOk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupFunc()
			gotVal, gotOk := ctx.Value(tt.key)
			if gotOk != tt.wantOk {
				t.Errorf("Value() ok = %v, want %v", gotOk, tt.wantOk)
			}
			if gotVal != tt.wantVal {
				t.Errorf("Value() val = %v, want %v", gotVal, tt.wantVal)
			}
		})
	}

	// Test with different types
	t.Run("int type retrieval", func(t *testing.T) {
		ctx := NewContexter[int]()
		ctx.SetValue("test", 42)

		if val, ok := ctx.Value("test"); !ok || val != 42 {
			t.Errorf("Value() failed to retrieve int value, got %v, want %v", val, 42)
		}
	})

	t.Run("struct type retrieval", func(t *testing.T) {
		type testStruct struct {
			field string
		}
		ctx := NewContexter[testStruct]()
		value := testStruct{field: "test"}
		ctx.SetValue("test", value)

		if val, ok := ctx.Value("test"); !ok || val.field != "test" {
			t.Errorf("Value() failed to retrieve struct value, got %v, want %v", val, value)
		}
	})
}

func TestContexter_Key_String(t *testing.T) {
	tests := []struct {
		name string
		key  *Key
		want string
	}{
		{
			name: "custom key",
			key:  &Key{Name: "custom"},
			want: "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.key.String(); got != tt.want {
				t.Errorf("Key.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContexter_Iterate(t *testing.T) {
	t.Run("iterate over string values", func(t *testing.T) {
		ctx := NewContexter[string]()
		expected := map[string]string{
			"key1": "value1",
			"key2": "value2",
			"key3": "value3",
		}

		// AddCleanup test values
		for k, v := range expected {
			ctx.SetValue(k, v)
		}

		// Collect values during iteration
		collected := make(map[string]string)
		ctx.Iterate(func(key string, value string) {
			collected[key] = value
		})

		// Compare maps
		if len(collected) != len(expected) {
			t.Errorf("Iterate() collected %d items, want %d", len(collected), len(expected))
		}

		for k, v := range expected {
			if collected[k] != v {
				t.Errorf("Iterate() value for key %s = %v, want %v", k, collected[k], v)
			}
		}
	})

	t.Run("iterate over empty contexter", func(t *testing.T) {
		ctx := NewContexter[string]()
		count := 0
		ctx.Iterate(func(_ string, _ string) {
			count++
		})
		if count != 0 {
			t.Errorf("Iterate() called function %d times on empty contexter, want 0", count)
		}
	})
}

func TestContexter_Len(t *testing.T) {
	ctx := NewContexter[string]()
	if ctx.Len() != 0 {
		t.Errorf("Len() = %d, want 0 for empty contexter", ctx.Len())
	}

	ctx.SetValue("key1", "value1")
	if ctx.Len() != 1 {
		t.Errorf("Len() = %d, want 1 after one insert", ctx.Len())
	}

	ctx.SetValue("key2", "value2")
	if ctx.Len() != 2 {
		t.Errorf("Len() = %d, want 2 after two inserts", ctx.Len())
	}

	ctx.SetValue("key1", "value3")
	if ctx.Len() != 2 {
		t.Errorf("Len() = %d, want 2 after overwrite", ctx.Len())
	}
}

func TestContexter_Clone(t *testing.T) {
	original := NewContexter[string]()
	original.SetValue("key1", "value1")
	original.SetValue("key2", "value2")

	cloned := original.Clone()
	if cloned == nil {
		t.Fatal("Clone() returned nil")
	}

	if cloned.Len() != original.Len() {
		t.Errorf("Clone().Len() = %d, want %d", cloned.Len(), original.Len())
	}

	val1, ok1 := cloned.Value("key1")
	if !ok1 || val1 != "value1" {
		t.Errorf("cloned.Value(key1) = %v, %v, want value1, true", val1, ok1)
	}

	val2, ok2 := cloned.Value("key2")
	if !ok2 || val2 != "value2" {
		t.Errorf("cloned.Value(key2) = %v, %v, want value2, true", val2, ok2)
	}

	original.SetValue("key3", "value3")
	val3, ok3 := cloned.Value("key3")
	if ok3 || val3 != "" {
		t.Errorf("cloned.Value(key3) = %v, %v, want empty, false (should be independent)", val3, ok3)
	}

	cloned.SetValue("key4", "value4")
	val4, ok4 := original.Value("key4")
	if ok4 || val4 != "" {
		t.Errorf("original.Value(key4) = %v, %v, want empty, false (should be independent)", val4, ok4)
	}
}
