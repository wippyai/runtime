// SPDX-License-Identifier: MPL-2.0

package code

import (
	"testing"

	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
)

func testProcessModule() *luaapi.ModuleDef {
	return &luaapi.ModuleDef{
		Name: "process",
		Types: func() *io.Manifest {
			m := io.NewManifest("process")
			moduleType := typ.NewInterface("process", []typ.Method{
				{
					Name: "send",
					Type: typ.Func().
						Param("pid", typ.String).
						Param("topic", typ.String).
						Variadic(typ.Any).
						Returns(typ.Boolean).
						Build(),
				},
			})
			m.SetExport(moduleType)
			return m
		},
	}
}

func TestTypeChecker_RequireBuiltinModuleManifest(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, []*luaapi.ModuleDef{testProcessModule()})

	source := `
local proc = require("process")
proc.send(123, "topic")
`

	_, diags, _ := tc.Check(source, "test.lua", nil)
	for _, d := range diags {
		if d.Severity == diag.SeverityError {
			return
		}
	}
	t.Fatal("expected type error for invalid process.send arguments")
}

func testHTTPModule() *luaapi.ModuleDef {
	return &luaapi.ModuleDef{
		Name: "http",
		Types: func() *io.Manifest {
			m := io.NewManifest("http")
			moduleType := typ.NewInterface("http", []typ.Method{
				{
					Name: "request",
					Type: typ.Func().
						OptParam("config", typ.Any).
						Returns(typ.Any).
						Build(),
				},
			})
			m.SetExport(moduleType)
			return m
		},
	}
}

func testHTTPClientModule() *luaapi.ModuleDef {
	return &luaapi.ModuleDef{
		Name: "http_client",
		Types: func() *io.Manifest {
			m := io.NewManifest("http_client")
			moduleType := typ.NewInterface("http_client", []typ.Method{
				{
					Name: "request",
					Type: typ.Func().
						Param("method", typ.String).
						Param("url", typ.String).
						OptParam("opts", typ.Any).
						Returns(typ.Any).
						Build(),
				},
			})
			m.SetExport(moduleType)
			return m
		},
	}
}

func TestTypeChecker_RequireHTTPClientDoesNotShadowHTTP(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, []*luaapi.ModuleDef{
		testHTTPModule(),
		testHTTPClientModule(),
	})

	source := `
local function main()
    local http = require("http_client")
    local resp = http.request("OPTIONS", "http://localhost/test", {})
end
`

	_, diags, _ := tc.Check(source, "test.lua", nil)
	for _, d := range diags {
		if d.Severity == diag.SeverityError {
			t.Errorf("unexpected error: %s at %d:%d", d.Message, d.Position.Line, d.Position.Column)
		}
	}
}
