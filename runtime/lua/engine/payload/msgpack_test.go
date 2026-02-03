package payload

import (
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
)

func TestToMsgPack_Transcode(t *testing.T) {
	tests := []struct {
		value   lua.LValue
		name    string
		wantErr bool
	}{
		{
			name:    "string",
			value:   lua.LString("hello"),
			wantErr: false,
		},
		{
			name:    "number",
			value:   lua.LNumber(3.14),
			wantErr: false,
		},
		{
			name:    "integer",
			value:   lua.LNumber(42),
			wantErr: false,
		},
		{
			name:    "bool_true",
			value:   lua.LTrue,
			wantErr: false,
		},
		{
			name:    "bool_false",
			value:   lua.LFalse,
			wantErr: false,
		},
		{
			name:    "nil",
			value:   lua.LNil,
			wantErr: false,
		},
	}

	transcoder := &ToMsgPack{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := payload.NewPayload(tt.value, payload.Lua)
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

func TestToMsgPack_Table(t *testing.T) {
	transcoder := &ToMsgPack{}

	// Create a Lua table
	tbl := lua.CreateTable(0, 2)
	tbl.RawSetString("key", lua.LString("value"))
	tbl.RawSetString("num", lua.LNumber(42))

	p := payload.NewPayload(tbl, payload.Lua)
	result, err := transcoder.Transcode(p)

	if err != nil {
		t.Fatalf("ToMsgPack.Transcode() error = %v", err)
	}

	if result.Format() != payload.MsgPack {
		t.Errorf("ToMsgPack.Transcode() format = %v, want %v", result.Format(), payload.MsgPack)
	}
}

func TestToMsgPack_InvalidFormat(t *testing.T) {
	transcoder := &ToMsgPack{}
	p := payload.NewPayload("test", payload.String)
	_, err := transcoder.Transcode(p)
	if err == nil {
		t.Error("ToMsgPack.Transcode() expected error for non-Lua format")
	}
}

func TestMsgPackToLua_Transcode(t *testing.T) {
	toMsgPack := &ToMsgPack{}
	fromMsgPack := &MsgPackToLua{}

	tests := []struct {
		value lua.LValue
		name  string
	}{
		{lua.LString("hello"), "string"},
		{lua.LNumber(3.14), "number"},
		{lua.LTrue, "bool"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode to msgpack
			p := payload.NewPayload(tt.value, payload.Lua)
			encoded, err := toMsgPack.Transcode(p)
			if err != nil {
				t.Fatalf("ToMsgPack.Transcode() error = %v", err)
			}

			// Decode back
			decoded, err := fromMsgPack.Transcode(encoded)
			if err != nil {
				t.Fatalf("MsgPackToLua.Transcode() error = %v", err)
			}

			if decoded.Format() != payload.Lua {
				t.Errorf("MsgPackToLua.Transcode() format = %v, want %v", decoded.Format(), payload.Lua)
			}

			_, ok := decoded.Data().(lua.LValue)
			if !ok {
				t.Errorf("MsgPackToLua.Transcode() data type = %T, want lua.LValue", decoded.Data())
			}
		})
	}
}

func TestMsgPackToLua_InvalidFormat(t *testing.T) {
	transcoder := &MsgPackToLua{}
	p := payload.NewPayload("test", payload.String)
	_, err := transcoder.Transcode(p)
	if err == nil {
		t.Error("MsgPackToLua.Transcode() expected error for non-MsgPack format")
	}
}

func TestMsgPackToLua_InvalidData(t *testing.T) {
	transcoder := &MsgPackToLua{}
	p := payload.NewPayload("not bytes", payload.MsgPack)
	_, err := transcoder.Transcode(p)
	if err == nil {
		t.Error("MsgPackToLua.Transcode() expected error for non-[]byte data")
	}
}

func TestLuaMsgPackRoundTrip(t *testing.T) {
	toMsgPack := &ToMsgPack{}
	fromMsgPack := &MsgPackToLua{}

	// Create a Lua table
	tbl := lua.CreateTable(0, 3)
	tbl.RawSetString("name", lua.LString("test"))
	tbl.RawSetString("count", lua.LNumber(42))
	tbl.RawSetString("enabled", lua.LTrue)

	// Nested table
	nested := lua.CreateTable(0, 2)
	nested.RawSetString("x", lua.LNumber(1))
	nested.RawSetString("y", lua.LNumber(2))
	tbl.RawSetString("nested", nested)

	// Array
	arr := lua.CreateTable(3, 0)
	arr.RawSetInt(1, lua.LString("a"))
	arr.RawSetInt(2, lua.LString("b"))
	arr.RawSetInt(3, lua.LString("c"))
	tbl.RawSetString("tags", arr)

	p := payload.NewPayload(tbl, payload.Lua)

	// Encode
	encoded, err := toMsgPack.Transcode(p)
	if err != nil {
		t.Fatalf("ToMsgPack.Transcode() error = %v", err)
	}

	// Decode
	decoded, err := fromMsgPack.Transcode(encoded)
	if err != nil {
		t.Fatalf("MsgPackToLua.Transcode() error = %v", err)
	}

	// Verify it's a table
	result, ok := decoded.Data().(*lua.LTable)
	if !ok {
		t.Fatalf("decoded data type = %T, want *lua.LTable", decoded.Data())
	}

	// Check values
	if result.RawGetString("name").String() != "test" {
		t.Errorf("result[name] = %v, want test", result.RawGetString("name"))
	}

	if float64(result.RawGetString("count").(lua.LNumber)) != 42 {
		t.Errorf("result[count] = %v, want 42", result.RawGetString("count"))
	}

	if result.RawGetString("enabled") != lua.LTrue {
		t.Errorf("result[enabled] = %v, want true", result.RawGetString("enabled"))
	}
}

// Benchmarks

func BenchmarkToMsgPack_Simple(b *testing.B) {
	transcoder := &ToMsgPack{}
	p := payload.NewPayload(lua.LString("hello world"), payload.Lua)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := transcoder.Transcode(p)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkToMsgPack_Table(b *testing.B) {
	transcoder := &ToMsgPack{}

	tbl := lua.CreateTable(0, 5)
	tbl.RawSetString("name", lua.LString("benchmark"))
	tbl.RawSetString("count", lua.LNumber(42))
	tbl.RawSetString("enabled", lua.LTrue)
	tbl.RawSetString("ratio", lua.LNumber(3.14))

	arr := lua.CreateTable(3, 0)
	arr.RawSetInt(1, lua.LString("a"))
	arr.RawSetInt(2, lua.LString("b"))
	arr.RawSetInt(3, lua.LString("c"))
	tbl.RawSetString("tags", arr)

	p := payload.NewPayload(tbl, payload.Lua)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := transcoder.Transcode(p)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkToMsgPack_NestedTable(b *testing.B) {
	transcoder := &ToMsgPack{}

	nested := lua.CreateTable(0, 2)
	nested.RawSetString("x", lua.LNumber(1))
	nested.RawSetString("y", lua.LNumber(2))

	arr := lua.CreateTable(5, 0)
	for i := 1; i <= 5; i++ {
		arr.RawSetInt(i, lua.LNumber(float64(i)))
	}

	root := lua.CreateTable(0, 3)
	root.RawSetString("nested", nested)
	root.RawSetString("array", arr)
	root.RawSetString("value", lua.LNumber(42))

	p := payload.NewPayload(root, payload.Lua)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := transcoder.Transcode(p)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMsgPackToLua_Simple(b *testing.B) {
	toMsgPack := &ToMsgPack{}
	fromMsgPack := &MsgPackToLua{}

	p := payload.NewPayload(lua.LString("hello world"), payload.Lua)
	encoded, _ := toMsgPack.Transcode(p)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := fromMsgPack.Transcode(encoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMsgPackToLua_Table(b *testing.B) {
	toMsgPack := &ToMsgPack{}
	fromMsgPack := &MsgPackToLua{}

	tbl := lua.CreateTable(0, 5)
	tbl.RawSetString("name", lua.LString("benchmark"))
	tbl.RawSetString("count", lua.LNumber(42))
	tbl.RawSetString("enabled", lua.LTrue)
	tbl.RawSetString("ratio", lua.LNumber(3.14))

	p := payload.NewPayload(tbl, payload.Lua)
	encoded, _ := toMsgPack.Transcode(p)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := fromMsgPack.Transcode(encoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLuaMsgPackRoundTrip_Simple(b *testing.B) {
	toMsgPack := &ToMsgPack{}
	fromMsgPack := &MsgPackToLua{}

	p := payload.NewPayload(lua.LString("hello world"), payload.Lua)

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

func BenchmarkLuaMsgPackRoundTrip_Table(b *testing.B) {
	toMsgPack := &ToMsgPack{}
	fromMsgPack := &MsgPackToLua{}

	tbl := lua.CreateTable(0, 5)
	tbl.RawSetString("name", lua.LString("benchmark"))
	tbl.RawSetString("count", lua.LNumber(42))
	tbl.RawSetString("enabled", lua.LTrue)
	tbl.RawSetString("ratio", lua.LNumber(3.14))

	p := payload.NewPayload(tbl, payload.Lua)

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
