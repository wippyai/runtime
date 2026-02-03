package msgpack

import (
	"testing"

	"github.com/wippyai/runtime/api/payload"
)

func TestToMsgPack_Transcode(t *testing.T) {
	tests := []struct {
		input   any
		name    string
		wantErr bool
	}{
		{
			name:    "string",
			input:   "hello",
			wantErr: false,
		},
		{
			name:    "int",
			input:   42,
			wantErr: false,
		},
		{
			name:    "float",
			input:   3.14,
			wantErr: false,
		},
		{
			name:    "bool",
			input:   true,
			wantErr: false,
		},
		{
			name:    "nil",
			input:   nil,
			wantErr: false,
		},
		{
			name:    "slice",
			input:   []any{"a", "b", "c"},
			wantErr: false,
		},
		{
			name:    "map",
			input:   map[string]any{"key": "value", "num": 123},
			wantErr: false,
		},
		{
			name:    "nested",
			input:   map[string]any{"list": []any{1, 2, 3}, "nested": map[string]any{"a": 1}},
			wantErr: false,
		},
	}

	transcoder := &ToMsgPack{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := payload.NewPayload(tt.input, payload.Golang)
			result, err := transcoder.Transcode(p)

			if (err != nil) != tt.wantErr {
				t.Errorf("ToMsgPack.Transcode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil {
				if result.Format() != payload.MsgPack {
					t.Errorf("ToMsgPack.Transcode() format = %v, want %v", result.Format(), payload.MsgPack)
				}
				if _, ok := result.Data().([]byte); !ok {
					t.Errorf("ToMsgPack.Transcode() data type = %T, want []byte", result.Data())
				}
			}
		})
	}
}

func TestToMsgPack_InvalidFormat(t *testing.T) {
	transcoder := &ToMsgPack{}
	p := payload.NewPayload("test", payload.String)
	_, err := transcoder.Transcode(p)
	if err == nil {
		t.Error("ToMsgPack.Transcode() expected error for non-Golang format")
	}
}

func TestFromMsgPack_Transcode(t *testing.T) {
	toMsgPack := &ToMsgPack{}
	fromMsgPack := &FromMsgPack{}

	tests := []struct {
		input any
		name  string
	}{
		{"hello", "string"},
		{42, "int"},
		{3.14, "float"},
		{true, "bool"},
		{nil, "nil"},
		{[]any{"a", "b", "c"}, "slice"},
		{map[string]any{"key": "value", "num": int64(123)}, "map"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode to msgpack
			p := payload.NewPayload(tt.input, payload.Golang)
			encoded, err := toMsgPack.Transcode(p)
			if err != nil {
				t.Fatalf("ToMsgPack.Transcode() error = %v", err)
			}

			// Decode back
			decoded, err := fromMsgPack.Transcode(encoded)
			if err != nil {
				t.Fatalf("FromMsgPack.Transcode() error = %v", err)
			}

			if decoded.Format() != payload.Golang {
				t.Errorf("FromMsgPack.Transcode() format = %v, want %v", decoded.Format(), payload.Golang)
			}
		})
	}
}

func TestFromMsgPack_InvalidFormat(t *testing.T) {
	transcoder := &FromMsgPack{}
	p := payload.NewPayload("test", payload.String)
	_, err := transcoder.Transcode(p)
	if err == nil {
		t.Error("FromMsgPack.Transcode() expected error for non-MsgPack format")
	}
}

func TestFromMsgPack_InvalidData(t *testing.T) {
	transcoder := &FromMsgPack{}
	p := payload.NewPayload("not bytes", payload.MsgPack)
	_, err := transcoder.Transcode(p)
	if err == nil {
		t.Error("FromMsgPack.Transcode() expected error for non-[]byte data")
	}
}

func TestRoundTrip(t *testing.T) {
	toMsgPack := &ToMsgPack{}
	fromMsgPack := &FromMsgPack{}

	original := map[string]any{
		"name":    "test",
		"count":   int64(42),
		"enabled": true,
		"tags":    []any{"a", "b", "c"},
		"nested": map[string]any{
			"x": int64(1),
			"y": int64(2),
		},
	}

	p := payload.NewPayload(original, payload.Golang)

	// Encode
	encoded, err := toMsgPack.Transcode(p)
	if err != nil {
		t.Fatalf("ToMsgPack.Transcode() error = %v", err)
	}

	// Decode
	decoded, err := fromMsgPack.Transcode(encoded)
	if err != nil {
		t.Fatalf("FromMsgPack.Transcode() error = %v", err)
	}

	// Verify structure
	result, ok := decoded.Data().(map[string]any)
	if !ok {
		t.Fatalf("decoded data type = %T, want map[string]any", decoded.Data())
	}

	// msgpack decodes strings as []byte, check both cases
	switch name := result["name"].(type) {
	case string:
		if name != "test" {
			t.Errorf("result[name] = %v, want test", name)
		}
	case []byte:
		if string(name) != "test" {
			t.Errorf("result[name] = %v, want test", string(name))
		}
	default:
		t.Errorf("result[name] type = %T, want string or []byte", result["name"])
	}

	if result["enabled"] != true {
		t.Errorf("result[enabled] = %v, want true", result["enabled"])
	}
}

// Benchmarks

func BenchmarkToMsgPack_Simple(b *testing.B) {
	transcoder := &ToMsgPack{}
	p := payload.NewPayload("hello world", payload.Golang)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := transcoder.Transcode(p)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkToMsgPack_Map(b *testing.B) {
	transcoder := &ToMsgPack{}
	data := map[string]any{
		"name":    "benchmark",
		"count":   42,
		"enabled": true,
		"ratio":   3.14,
		"tags":    []any{"a", "b", "c"},
	}
	p := payload.NewPayload(data, payload.Golang)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := transcoder.Transcode(p)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkToMsgPack_Nested(b *testing.B) {
	transcoder := &ToMsgPack{}
	data := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"level3": []any{1, 2, 3, 4, 5},
			},
		},
	}
	p := payload.NewPayload(data, payload.Golang)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := transcoder.Transcode(p)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFromMsgPack_Simple(b *testing.B) {
	toMsgPack := &ToMsgPack{}
	fromMsgPack := &FromMsgPack{}

	p := payload.NewPayload("hello world", payload.Golang)
	encoded, _ := toMsgPack.Transcode(p)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := fromMsgPack.Transcode(encoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFromMsgPack_Map(b *testing.B) {
	toMsgPack := &ToMsgPack{}
	fromMsgPack := &FromMsgPack{}

	data := map[string]any{
		"name":    "benchmark",
		"count":   42,
		"enabled": true,
		"ratio":   3.14,
		"tags":    []any{"a", "b", "c"},
	}
	p := payload.NewPayload(data, payload.Golang)
	encoded, _ := toMsgPack.Transcode(p)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := fromMsgPack.Transcode(encoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRoundTrip_Simple(b *testing.B) {
	toMsgPack := &ToMsgPack{}
	fromMsgPack := &FromMsgPack{}

	p := payload.NewPayload("hello world", payload.Golang)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded, err := toMsgPack.Transcode(p)
		if err != nil {
			b.Fatal(err)
		}
		_, err = fromMsgPack.Transcode(encoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRoundTrip_Map(b *testing.B) {
	toMsgPack := &ToMsgPack{}
	fromMsgPack := &FromMsgPack{}

	data := map[string]any{
		"name":    "benchmark",
		"count":   42,
		"enabled": true,
		"ratio":   3.14,
		"tags":    []any{"a", "b", "c"},
	}
	p := payload.NewPayload(data, payload.Golang)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded, err := toMsgPack.Transcode(p)
		if err != nil {
			b.Fatal(err)
		}
		_, err = fromMsgPack.Transcode(encoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}
