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
		ctx.WithValue("test", "value")

		if val, ok := ctx.shared["test"]; !ok || val != "value" {
			t.Errorf("WithValue() failed to store string value, got %v, want %v", val, "value")
		}
	})

	t.Run("int type", func(t *testing.T) {
		ctx := NewContexter[int]()
		ctx.WithValue("test", 42)

		if val, ok := ctx.shared["test"]; !ok || val != 42 {
			t.Errorf("WithValue() failed to store int value, got %v, want %v", val, 42)
		}
	})

	t.Run("struct type", func(t *testing.T) {
		type testStruct struct {
			field string
		}
		ctx := NewContexter[testStruct]()
		value := testStruct{field: "test"}
		ctx.WithValue("test", value)

		if val, ok := ctx.shared["test"]; !ok || val.field != "test" {
			t.Errorf("WithValue() failed to store struct value, got %v, want %v", val, value)
		}
	})

	t.Run("overwrite existing value", func(t *testing.T) {
		ctx := NewContexter[string]()
		ctx.WithValue("test", "value1")
		ctx.WithValue("test", "value2")

		if val, ok := ctx.shared["test"]; !ok || val != "value2" {
			t.Errorf("WithValue() failed to overwrite value, got %v, want %v", val, "value2")
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
				ctx.WithValue("test", "value")
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
		ctx.WithValue("test", 42)

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
		ctx.WithValue("test", value)

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
			name: "bus context key",
			key:  BusCtx,
			want: "bus",
		},
		{
			name: "transcoder context key",
			key:  TranscoderCtx,
			want: "transcoder",
		},
		{
			name: "executor context key",
			key:  ExecutorCtx,
			want: "executor",
		},
		{
			name: "logger context key",
			key:  LoggerCtx,
			want: "logger",
		},
		{
			name: "values context key",
			key:  ValuesCtx,
			want: "values",
		},
		{
			name: "cleanup context key",
			key:  CleanupCtx,
			want: "cleanup",
		},
		{
			name: "task context key",
			key:  TaskCtx,
			want: "task",
		},
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
