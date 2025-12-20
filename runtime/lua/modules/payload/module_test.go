package payload

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestLoad(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	mod := l.GetGlobal("payload")
	if mod.Type() != lua.LTTable {
		t.Fatal("payload module not registered")
	}

	tbl := mod.(*lua.LTable)
	if tbl.RawGetString("new").Type() != lua.LTFunction {
		t.Error("new function not registered")
	}
	if tbl.RawGetString("format").Type() != lua.LTTable {
		t.Error("format table not registered")
	}
}

func TestLoadReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	Module.Load(l1)
	Module.Load(l2)

	mod1 := l1.GetGlobal("payload").(*lua.LTable)
	mod2 := l2.GetGlobal("payload").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestFormatConstants(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local formats = payload.format
		if formats.JSON ~= "json/plain" then
			error("JSON format incorrect")
		end
		if formats.YAML ~= "yaml/plain" then
			error("YAML format incorrect")
		end
		if formats.STRING ~= "text/plain" then
			error("STRING format incorrect")
		end
		if formats.BYTES ~= "application/octet-stream" then
			error("BYTES format incorrect")
		end
		if formats.LUA ~= "lua/any" then
			error("LUA format incorrect")
		end
		if formats.GOLANG ~= "golang/any" then
			error("GOLANG format incorrect")
		end
		if formats.ERROR ~= "golang/error" then
			error("ERROR format incorrect")
		end
	`)
	if err != nil {
		t.Errorf("format constants test failed: %v", err)
	}
}

func TestNewPayload(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local p = payload.new({key = "value"})
		if p == nil then
			error("expected payload object")
		end
		if p:get_format() ~= payload.format.LUA then
			error("expected LUA format, got: " .. p:get_format())
		end
	`)
	if err != nil {
		t.Errorf("new payload test failed: %v", err)
	}
}

func TestPayloadToString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local p = payload.new("hello")
		local str = tostring(p)
		if not string.find(str, "payload") then
			error("tostring should contain 'payload'")
		end
		if not string.find(str, "lua/any") then
			error("tostring should contain format")
		end
	`)
	if err != nil {
		t.Errorf("tostring test failed: %v", err)
	}
}

func TestPayloadTypes(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	tests := []struct {
		name  string
		value string
	}{
		{"string", `"hello"`},
		{"number", `123`},
		{"boolean", `true`},
		{"table", `{a = 1, b = 2}`},
		{"array", `{1, 2, 3}`},
		{"nil", `nil`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := l.DoString(`
				local p = payload.new(` + tt.value + `)
				if p == nil then
					error("expected payload object for ` + tt.name + `")
				end
			`)
			if err != nil {
				t.Errorf("failed for %s: %v", tt.name, err)
			}
		})
	}
}
