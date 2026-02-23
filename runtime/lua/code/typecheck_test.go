// SPDX-License-Identifier: MPL-2.0

package code

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua/types/constraint"
	"github.com/wippyai/go-lua/types/contract"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/query/core"
	"github.com/wippyai/go-lua/types/typ"
	base64mod "github.com/wippyai/runtime/runtime/lua/modules/base64"
)

func testModuleTypes() *io.Manifest {
	m := io.NewManifest("mylib")

	addFn := typ.Func().
		Param("a", typ.Number).
		Param("b", typ.Number).
		Returns(typ.Number).
		Build()

	concatFn := typ.Func().
		Param("s1", typ.String).
		Param("s2", typ.String).
		Returns(typ.String).
		Build()

	greetFn := typ.Func().
		Param("name", typ.String).
		Returns(typ.String, typ.NewOptional(typ.LuaError)).
		Build()

	moduleType := typ.NewRecord().
		Field("add", addFn).
		Field("concat", concatFn).
		Field("greet", greetFn).
		Build()

	m.SetExport(moduleType)
	return m
}

func TestTypeChecker_ModuleMethodCall_Valid(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, nil)

	imports := map[string]*io.Manifest{
		"mylib": testModuleTypes(),
	}

	source := `
local mylib = require("mylib")

local x: number = mylib.add(1, 2)
local s: string = mylib.concat("hello", "world")
`

	manifest, diagnostics, err := tc.Check(source, "test.lua", imports)
	require.NoError(t, err)
	require.NotNil(t, manifest)

	for _, d := range diagnostics {
		if d.Severity == diag.SeverityError {
			t.Logf("Error: %s at %d:%d", d.Message, d.Position.Line, d.Position.Column)
		}
	}
	assert.False(t, HasErrors(diagnostics), "expected no type errors")
}

func TestTypeChecker_ModuleMethodCall_WrongArgType(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, nil)

	imports := map[string]*io.Manifest{
		"mylib": testModuleTypes(),
	}

	source := `
local mylib = require("mylib")

local x = mylib.add("not a number", 2)
`

	_, diagnostics, err := tc.Check(source, "test.lua", imports)
	require.NoError(t, err)
	require.True(t, HasErrors(diagnostics), "expected type error for wrong argument type")

	found := false
	for _, d := range diagnostics {
		if d.Severity == diag.SeverityError {
			t.Logf("Error at line %d: %s", d.Position.Line, d.Message)
			if d.Position.Line == 4 {
				found = true
			}
		}
	}
	assert.True(t, found, "expected error on line 4 where wrong argument is passed")
}

func TestTypeChecker_ModuleMethodCall_WrongReturnAssignment(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, nil)

	imports := map[string]*io.Manifest{
		"mylib": testModuleTypes(),
	}

	source := `
local mylib = require("mylib")

local s: string = mylib.add(1, 2)
`

	_, diagnostics, err := tc.Check(source, "test.lua", imports)
	require.NoError(t, err)
	require.True(t, HasErrors(diagnostics), "expected type error for wrong return assignment")

	for _, d := range diagnostics {
		if d.Severity == diag.SeverityError {
			t.Logf("Error at line %d: %s", d.Position.Line, d.Message)
		}
	}
}

func TestTypeChecker_ModuleMethodCall_UndefinedMethod(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, nil)

	imports := map[string]*io.Manifest{
		"mylib": testModuleTypes(),
	}

	source := `
local mylib = require("mylib")

local x = mylib.nonexistent(1, 2)
`

	_, diagnostics, err := tc.Check(source, "test.lua", imports)
	require.NoError(t, err)
	require.True(t, HasErrors(diagnostics), "expected type error for undefined method")

	found := false
	for _, d := range diagnostics {
		if d.Severity == diag.SeverityError {
			t.Logf("Error at line %d: %s", d.Position.Line, d.Message)
			if d.Position.Line == 4 {
				found = true
			}
		}
	}
	assert.True(t, found, "expected error on line 4 where undefined method is called")
}

func TestTypeChecker_LibraryChunk_WithManifest(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: false}, nil)

	libSource := `
---@param x number
---@param y number
---@return number
local function add(x, y)
    return x + y
end

---@param s string
---@return string
local function upper(s)
    return string.upper(s)
end

return {
    add = add,
    upper = upper
}
`

	libManifest, diagnostics, err := tc.Check(libSource, "mathlib.lua", nil)
	require.NoError(t, err)

	for _, d := range diagnostics {
		if d.Severity == diag.SeverityError {
			t.Logf("Library error: %s", d.Message)
		}
	}
	require.NotNil(t, libManifest)

	t.Logf("Library manifest export: %v", libManifest.Export)
}

func TestTypeChecker_ManifestExport(t *testing.T) {
	manifest := testModuleTypes()

	require.NotNil(t, manifest.Export)

	addType, ok := core.FieldOrMethod(manifest.Export, "add")
	require.True(t, ok)
	addFn, ok := addType.(*typ.Function)
	require.True(t, ok)
	assert.Len(t, addFn.Params, 2)
	assert.Len(t, addFn.Returns, 1)

	concatType, ok := core.FieldOrMethod(manifest.Export, "concat")
	require.True(t, ok)
	concatFn, ok := concatType.(*typ.Function)
	require.True(t, ok)
	assert.Len(t, concatFn.Params, 2)
}

func TestTypeChecker_Base64Import(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, nil)

	imports := map[string]*io.Manifest{
		"base64": base64mod.ModuleTypes(),
	}

	code := `
local base64 = require("base64")

local function test_encode(): boolean
    local input: string = "hello world"
    local encoded: string = base64.encode(input)
    return encoded == "aGVsbG8gd29ybGQ="
end

return { test_encode = test_encode }
`

	_, diagnostics, err := tc.Check(code, "test.lua", imports)
	require.NoError(t, err)

	for _, d := range diagnostics {
		if d.Severity == diag.SeverityError {
			t.Errorf("unexpected error: %s", d.Message)
		}
	}
}

func testSubmoduleTypes() *io.Manifest {
	m := io.NewManifest("mymod")

	widgetType := typ.NewRecord().
		Field("process", typ.Func().Param("s", typ.String).Returns(typ.Boolean).Build()).
		Field("value", typ.Func().Returns(typ.String).Build()).
		Build()

	factoryType := typ.NewRecord().
		Field("create", typ.Func().
			Param("name", typ.String).
			Returns(widgetType, typ.NewOptional(typ.LuaError)).
			Build()).
		Build()

	m.DefineType("Widget", widgetType)

	moduleType := typ.NewRecord().
		Field("factory", factoryType).
		Build()

	m.SetExport(moduleType)
	return m
}

func TestTypeChecker_SubmoduleAccess(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, nil)

	manifest := testSubmoduleTypes()
	imports := map[string]*io.Manifest{
		"mymod": manifest,
	}

	require.NotNil(t, manifest.Export, "manifest export should not be nil")

	factoryField, ok := core.FieldOrMethod(manifest.Export, "factory")
	require.True(t, ok, "should have factory field")
	require.NotNil(t, factoryField)

	createMethod, ok := core.FieldOrMethod(factoryField, "create")
	require.True(t, ok, "factory should have create method")
	require.NotNil(t, createMethod)
	t.Logf("create method: %v", createMethod)

	code := `
local mymod = require("mymod")
local widget = mymod.factory.create("test")
local s: string = widget:value()
return s
`

	_, diagnostics, err := tc.Check(code, "test.lua", imports)
	require.NoError(t, err)

	for _, d := range diagnostics {
		if d.Severity == diag.SeverityError {
			t.Errorf("unexpected error: %s", d.Message)
		}
	}
}

func TestTypeChecker_Clone(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, nil)
	clone := tc.Clone()

	require.NotNil(t, clone)
	require.NotSame(t, tc, clone)
	require.NotSame(t, tc.db, clone.db)
	require.NotSame(t, tc.checker, clone.checker)

	source := `
local x: number = 42
return x
`
	_, diags, err := clone.Check(source, "test.lua", nil)
	require.NoError(t, err)
	assert.False(t, HasErrors(diags))
}

func TestTypeChecker_WithConfig(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, nil)
	lenient := tc.WithConfig(TypeCheckConfig{Enabled: true, Strict: false})

	require.NotNil(t, lenient)
	require.NotSame(t, tc, lenient)
	assert.True(t, tc.IsStrict())
	assert.False(t, lenient.IsStrict())
}

func TestTypeChecker_AddBuiltinManifest(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, nil)

	customMod := io.NewManifest("custom")
	customFn := typ.Func().
		Param("x", typ.Number).
		Returns(typ.Number).
		Build()
	customMod.SetExport(typ.NewRecord().Field("double", customFn).Build())

	tc.AddBuiltinManifest("custom", customMod)

	// Clone to get fresh checker with updated base scope
	tc2 := tc.Clone()

	source := `
local x: number = custom.double(21)
return x
`
	_, diags, err := tc2.Check(source, "test.lua", nil)
	require.NoError(t, err)

	for _, d := range diags {
		if d.Severity == diag.SeverityError {
			t.Errorf("unexpected error: %s", d.Message)
		}
	}
}

// TestTypeChecker_ManifestTypeAlias tests that type aliases from manifests
// are structurally equivalent to their underlying types.
func TestTypeChecker_ManifestTypeAlias(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, nil)

	// Build manifest with Name alias (string)
	nameAlias := typ.NewAlias("mylib.Name", typ.String)

	// Build manifest with Handler alias (function)
	handlerAlias := typ.NewAlias("mylib.Handler", typ.Func().
		Param("s", typ.String).
		Returns(typ.String).
		Build())

	// Module with functions that use the aliases
	modType := typ.NewRecord().
		Field("greet", typ.Func().
			Param("name", nameAlias).
			Returns(typ.String).
			Build()).
		Field("apply", typ.Func().
			Param("handler", handlerAlias).
			Param("input", typ.String).
			Returns(typ.String).
			Build()).
		Field("make_name", typ.Func().
			Param("s", typ.String).
			Returns(nameAlias).
			Build()).
		Build()

	manifest := io.NewManifest("mylib")
	manifest.DefineType("Name", nameAlias)
	manifest.DefineType("Handler", handlerAlias)
	manifest.SetExport(modType)

	imports := map[string]*io.Manifest{
		"mylib": manifest,
	}

	tests := []struct {
		name      string
		code      string
		wantError bool
	}{
		{
			name: "string_literal_to_alias_param",
			code: `
local mylib = require("mylib")
local msg: string = mylib.greet("world")
return msg
`,
			wantError: false,
		},
		{
			name: "function_literal_to_alias_param",
			code: `
local mylib = require("mylib")
local result: string = mylib.apply(function(s: string): string return s:upper() end, "test")
return result
`,
			wantError: false,
		},
		{
			name: "alias_return_assigned_to_underlying",
			code: `
local mylib = require("mylib")
local name: string = mylib.make_name("hello")
return name
`,
			wantError: false,
		},
		{
			name: "alias_return_used_as_alias_param",
			code: `
local mylib = require("mylib")
local name = mylib.make_name("hello")
local msg: string = mylib.greet(name)
return msg
`,
			wantError: false,
		},
		{
			name: "wrong_type_rejected",
			code: `
local mylib = require("mylib")
local msg: string = mylib.greet(42)
return msg
`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diagnostics, err := tc.Check(tt.code, "test.lua", imports)
			require.NoError(t, err)

			hasError := HasErrors(diagnostics)
			if hasError != tt.wantError {
				var errMsgs []string
				for _, d := range diagnostics {
					if d.Severity == diag.SeverityError {
						errMsgs = append(errMsgs, d.Message)
					}
				}
				t.Errorf("wantError=%v, gotError=%v, errors=%v", tt.wantError, hasError, errMsgs)
			}
		})
	}
}

// TestTypeChecker_ContractSpecNarrowing tests that contract specs narrow return types
// and the narrowed type propagates to method calls on the result.
func TestTypeChecker_ContractSpecNarrowing(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, nil)

	// Message type like wippy process.Message
	messageType := typ.NewInterface("process.Message", []typ.Method{
		{Name: "from", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
		{Name: "topic", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
		{Name: "payload", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any).Build()},
	})

	// Channel generic
	channelElem := typ.NewTypeParam("T", nil)
	channelType := typ.NewInterface("channel.Channel", []typ.Method{
		{Name: "receive", Type: typ.Func().
			Param("self", typ.Self).
			Returns(channelElem, typ.Boolean).
			Build()},
	})
	channelGeneric := typ.NewGeneric("channel.Channel", []*typ.TypeParam{channelElem}, channelType)

	messageChannelType := typ.Instantiate(channelGeneric, messageType)
	rawChannelType := typ.Instantiate(channelGeneric, typ.Any)

	// Process module with listen using ParamPath(1) like wippy
	listenSpec := contract.NewSpec().WithReturnCase(
		constraint.FromConstraints(constraint.FieldEquals{
			Target: constraint.ParamPath(1),
			Field:  "message",
			Value:  typ.True,
		}),
		messageChannelType,
	)

	processModule := typ.NewRecord().
		Field("listen", typ.Func().
			Param("topic", typ.String).
			OptParam("options", typ.Any).
			Returns(rawChannelType, typ.NewOptional(typ.String)).
			Spec(listenSpec).
			Build()).
		Field("send", typ.Func().
			Param("target", typ.String).
			Param("payload", typ.Any).
			Build()).
		Build()

	processManifest := io.NewManifest("process")
	processManifest.SetExport(processModule)
	processManifest.DefineType("Message", messageType)

	chManifest := io.NewManifest("channel")
	chManifest.DefineType("Channel", channelGeneric)

	imports := map[string]*io.Manifest{
		"process": processManifest,
		"channel": chManifest,
	}

	tests := []struct {
		name      string
		code      string
		wantError bool
	}{
		{
			name: "spec_narrowing_full_chain",
			code: `
local process = require("process")
local ch = process.listen("increment", {message = true})
local msg, ok = ch:receive()
local reply_to: string = msg:from()
process.send(reply_to, "ack")
return true
`,
			wantError: false,
		},
		{
			name: "spec_narrowing_method_call",
			code: `
local process = require("process")
local ch = process.listen("topic", {message = true})
local msg, ok = ch:receive()
local s: string = msg:from()
return s
`,
			wantError: false,
		},
		{
			name: "spec_narrowing_in_conditional",
			code: `
local process = require("process")
local ch = process.listen("topic", {message = true})
local msg, ok = ch:receive()
if ok then
	local s: string = msg:from()
end
return true
`,
			wantError: false,
		},
		{
			name: "no_spec_narrowing_without_message_option",
			code: `
local process = require("process")
local ch = process.listen("events")
local val, ok = ch:receive()
local x = val
return true
`,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diagnostics, err := tc.Check(tt.code, "test.lua", imports)
			require.NoError(t, err)

			hasError := HasErrors(diagnostics)
			if hasError != tt.wantError {
				var errMsgs []string
				for _, d := range diagnostics {
					if d.Severity == diag.SeverityError {
						errMsgs = append(errMsgs, d.Message)
					}
				}
				t.Errorf("wantError=%v, gotError=%v, errors=%v", tt.wantError, hasError, errMsgs)
			}
		})
	}
}

// TestTypeChecker_ManifestFunctionAliasCallable tests that function type aliases
// from manifests are recognized as callable when returned.
func TestTypeChecker_ManifestFunctionAliasCallable(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, nil)

	// Callback = fun(s: string): string
	callbackAlias := typ.NewAlias("mylib.Callback", typ.Func().
		Param("s", typ.String).
		Returns(typ.String).
		Build())

	// Module with function that returns a Callback
	modType := typ.NewRecord().
		Field("get_callback", typ.Func().
			Returns(callbackAlias).
			Build()).
		Build()

	manifest := io.NewManifest("mylib")
	manifest.DefineType("Callback", callbackAlias)
	manifest.SetExport(modType)

	imports := map[string]*io.Manifest{
		"mylib": manifest,
	}

	tests := []struct {
		name      string
		code      string
		wantError bool
	}{
		{
			name: "call_function_alias_return",
			code: `
local mylib = require("mylib")
local cb = mylib.get_callback()
local result: string = cb("hello")
return result
`,
			wantError: false,
		},
		{
			name: "function_alias_wrong_arg_type",
			code: `
local mylib = require("mylib")
local cb = mylib.get_callback()
local result: string = cb(42)
return result
`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diagnostics, err := tc.Check(tt.code, "test.lua", imports)
			require.NoError(t, err)

			hasError := HasErrors(diagnostics)
			if hasError != tt.wantError {
				var errMsgs []string
				for _, d := range diagnostics {
					if d.Severity == diag.SeverityError {
						errMsgs = append(errMsgs, d.Message)
					}
				}
				t.Errorf("wantError=%v, gotError=%v, errors=%v", tt.wantError, hasError, errMsgs)
			}
		})
	}
}
