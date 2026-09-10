// SPDX-License-Identifier: MPL-2.0

package funcs_test

import (
	"testing"

	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/typ"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/modules/funcs"
	"github.com/wippyai/runtime/runtime/lua/modules/future"
)

// TestFutureManifestMatchesRegisteredMethods proves the funcs.Future type manifest
// and the methods the future package actually registers describe the same set.
//
// The Future type is declared in the funcs package's manifest, but its methods are
// registered in the separate future package. A method present in the manifest but
// absent from future.Methods is a phantom: the type system advertises
// Future:<method>, but the runtime never registers it, so calling it fails.
func TestFutureManifestMatchesRegisteredMethods(t *testing.T) {
	target, ok := funcs.ModuleTypes().LookupType("Future")
	if !ok {
		t.Fatal("Future type not found in funcs manifest")
	}

	iface, ok := target.(*typ.Interface)
	if !ok {
		t.Fatalf("Future manifest type is %T, want *typ.Interface", target)
	}

	registered := future.Methods

	manifest := make(map[string]bool, len(iface.Methods))
	for _, m := range iface.Methods {
		manifest[m.Name] = true
		if _, found := registered[m.Name]; !found {
			t.Errorf("funcs.Future manifest declares %q but the future package does not register it (phantom method; calling it fails at runtime)", m.Name)
		}
	}

	for name := range registered {
		if !manifest[name] {
			t.Errorf("future package registers %q but the funcs.Future manifest omits it (undocumented method)", name)
		}
	}
}

func TestFutureManifestChannelsAreUnknownChannels(t *testing.T) {
	target, ok := funcs.ModuleTypes().LookupType("Future")
	if !ok {
		t.Fatal("Future type not found in funcs manifest")
	}

	iface := target.(*typ.Interface)
	for _, name := range []string{"response", "channel"} {
		t.Run(name, func(t *testing.T) {
			var method *typ.Method
			for i := range iface.Methods {
				if iface.Methods[i].Name == name {
					method = &iface.Methods[i]
					break
				}
			}
			if method == nil {
				t.Fatalf("Future.%s method missing", name)
			}

			fn := method.Type
			if len(fn.Returns) != 1 {
				t.Fatalf("Future.%s type = %v, want one return", name, method.Type)
			}
			channel, ok := fn.Returns[0].(*typ.Instantiated)
			if !ok || channel.Generic.Name != "channel.Channel" {
				t.Fatalf("Future.%s return = %v, want channel.Channel<unknown>", name, fn.Returns[0])
			}
			if len(channel.TypeArgs) != 1 || !channel.TypeArgs[0].Equals(typ.Unknown) {
				t.Fatalf("Future.%s return = %v, want channel.Channel<unknown>", name, fn.Returns[0])
			}
		})
	}
}

func TestFutureResponseCanBeRetainedAndSelectedInStrictLua(t *testing.T) {
	source := `
local funcs = require("funcs")
local channel = require("channel")
type Channel = channel.Channel
type ResponseChannel = Channel<unknown>

local function select_response(future: funcs.Future): ResponseChannel?
    local response: ResponseChannel? = future:response()
    if not response then return nil end

    local selected = channel.select { response:case_receive() }
    if selected.channel == response then
        return response
    end
    return nil
end

return select_response
`

	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, []*luaapi.ModuleDef{
		engine.ChannelModule,
		funcs.Module,
	})
	_, diagnostics, err := tc.Check(source, "future_response.lua", nil)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == diag.SeverityError {
			t.Fatalf("unexpected strict type error at %d:%d: %s", diagnostic.Position.Line, diagnostic.Position.Column, diagnostic.Message)
		}
	}
}
