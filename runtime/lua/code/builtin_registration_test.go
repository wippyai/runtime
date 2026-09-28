// SPDX-License-Identifier: MPL-2.0

package code

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"go.uber.org/zap"
)

func TestTypeChecker_AddBuiltinManifestRegistersTypesImmediately(t *testing.T) {
	initial := io.NewManifest("initial")
	initial.DefineType("Existing", typ.String)
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, []*api.ModuleDef{{
		Name:  "initial",
		Types: func() *io.Manifest { return initial },
	}})
	manifest := io.NewManifest("custom")
	manifest.DefineType("Item", typ.String)
	manifest.SetExport(typ.NewRecord().Field("value", typ.String).Build())
	tc.AddBuiltinManifest("custom", manifest)
	_, diags, err := tc.Check("local value: Existing = 'text'\nreturn value", "existing.lua", nil)
	require.NoError(t, err)
	require.False(t, HasErrors(diags), "late registration must retain existing types: %v", diags)

	for _, name := range []string{"Item", "custom.Item"} {
		_, diags, err := tc.Check("local value: "+name+" = custom.value\nreturn value", "valid.lua", nil)
		require.NoError(t, err)
		require.False(t, HasErrors(diags), "%s: %v", name, diags)

		_, diags, err = tc.Check("local value: "+name+" = 42\nreturn value", "invalid.lua", nil)
		require.NoError(t, err)
		require.True(t, HasErrors(diags), "%s should retain its string type", name)
	}
}

func TestTypeChecker_AddBuiltinManifestReplacesNamespace(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, nil)
	original := io.NewManifest("custom")
	original.DefineType("Item", typ.String)
	original.DefineType("Removed", typ.String)
	original.Globals["removed_global"] = typ.String
	original.SetExport(typ.NewRecord().Field("value", typ.String).Build())
	tc.AddBuiltinManifest("custom", original)
	clone := tc.Clone()

	replacement := io.NewManifest("custom")
	replacement.DefineType("Item", typ.Number)
	replacement.SetExport(typ.NewRecord().Field("value", typ.Number).Build())
	tc.AddBuiltinManifest("custom", replacement)

	item, ok := tc.BuildEnv().LookupType("Item")
	require.True(t, ok)
	require.True(t, typ.TypeEquals(typ.Number, item))
	_, ok = tc.BuildEnv().LookupType("Removed")
	require.False(t, ok, "replacement must remove the old bare type")
	_, ok = tc.GlobalTypes()["removed_global"]
	require.False(t, ok, "replacement must remove the old global")
	for _, name := range []string{"Item", "custom.Item"} {
		_, diags, err := tc.Check("local value: "+name+" = custom.value\nreturn value", "replacement.lua", nil)
		require.NoError(t, err)
		require.False(t, HasErrors(diags), "%s: %v", name, diags)
	}

	require.Same(t, original, clone.BuiltinManifest("custom"), "clones retain their registered environment")
	_, diags, err := clone.Check("local value: Item = custom.value\nreturn value", "clone.lua", nil)
	require.NoError(t, err)
	require.False(t, HasErrors(diags), "%v", diags)
}

func TestTypeChecker_AddBuiltinManifestRecomputesAmbiguity(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, nil)
	first := io.NewManifest("first")
	first.DefineType("Item", typ.String)
	first.DefineType("number", typ.String)
	tc.AddBuiltinManifest("first", first)

	item, ok := tc.BuildEnv().LookupType("Item")
	require.True(t, ok)
	require.True(t, typ.TypeEquals(typ.String, item))

	second := io.NewManifest("second")
	second.DefineType("Item", typ.Number)
	tc.AddBuiltinManifest("second", second)
	_, ok = tc.BuildEnv().LookupType("Item")
	require.False(t, ok, "conflicting modules must not leave a stale bare binding")
	_, diags, err := tc.Check("local text: first.Item = 'text'\nlocal count: second.Item = 42\nreturn text, count", "qualified.lua", nil)
	require.NoError(t, err)
	require.False(t, HasErrors(diags), "%v", diags)

	replacement := io.NewManifest("second")
	replacement.DefineType("Other", typ.Number)
	tc.AddBuiltinManifest("second", replacement)
	item, ok = tc.BuildEnv().LookupType("Item")
	require.True(t, ok, "an unambiguous name must become available again")
	require.True(t, typ.TypeEquals(typ.String, item))
	number, ok := tc.BuildEnv().LookupType("number")
	require.True(t, ok)
	require.True(t, typ.TypeEquals(typ.Number, number), "builtin type names retain precedence")
}

func TestManager_AddBuiltinTypeRegistersTypesImmediately(t *testing.T) {
	cm, err := NewCodeManager(zap.NewNop(), &testEventBus{}, Config{})
	require.NoError(t, err)
	manifest := io.NewManifest("late")
	manifest.DefineType("Item", typ.String)
	cm.AddBuiltinType(&api.ModuleDef{
		Name:  "late",
		Types: func() *io.Manifest { return manifest },
	})

	item, ok := cm.GetTypeChecker().BuildEnv().LookupType("Item")
	require.True(t, ok)
	require.True(t, typ.TypeEquals(typ.String, item))
	_, diags, err := cm.GetTypeChecker().Check("local value: Item = 42\nreturn value", "late.lua", nil)
	require.NoError(t, err)
	require.True(t, HasErrors(diags), "manager registration must expose the named string type")
}

func TestTypeChecker_AddBuiltinManifestConcurrentReaders(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: true}, nil)
	manifest := io.NewManifest("custom")
	manifest.DefineType("Item", typ.String)
	tc.AddBuiltinManifest("custom", manifest)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 20 {
			tc.AddBuiltinManifest("custom", manifest)
		}
	}()
	for range 20 {
		tc.ClearCache()
		require.Same(t, manifest, tc.BuiltinManifest("custom"))
		_, ok := tc.BuildEnv().LookupType("Item")
		require.True(t, ok)
		require.NotEmpty(t, tc.GlobalTypes())
		_, diags, err := tc.Clone().Check("local value: Item = 'text'\nreturn value", "reader.lua", nil)
		require.NoError(t, err)
		require.False(t, HasErrors(diags), "%v", diags)
	}
	wg.Wait()
}
