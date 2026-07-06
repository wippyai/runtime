// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua/types/typ"
)

func recordField(t *testing.T, r *typ.Record, name string) typ.Type {
	t.Helper()
	for _, f := range r.Fields {
		if f.Name == name {
			return f.Type
		}
	}
	t.Fatalf("record has no field %q", name)
	return nil
}

func ifaceMethod(t *testing.T, i *typ.Interface, name string) *typ.Function {
	t.Helper()
	for _, m := range i.Methods {
		if m.Name == name {
			return m.Type
		}
	}
	t.Fatalf("interface has no method %q", name)
	return nil
}

// TestManifestTypesReadSurface walks the manifest and proves hub.versions.open
// returns a typed package handle (a record whose methods are declared), not
// typ.Any, and that hub.cache.list returns a typed array.
func TestManifestTypesReadSurface(t *testing.T) {
	export, ok := ModuleTypes().EnrichedExport().(*typ.Record)
	require.True(t, ok, "hub export is not a record")

	versions, ok := recordField(t, export, "versions").(*typ.Interface)
	require.True(t, ok, "hub.versions is not an interface")

	open := ifaceMethod(t, versions, "open")
	require.Lenf(t, open.Returns, 2, "open should return (handle, error?), got %d returns", len(open.Returns))

	handle, ok := open.Returns[0].(*typ.Record)
	require.Truef(t, ok, "open returns %T, want a typed *typ.Record handle (not Any)", open.Returns[0])

	// scalar fields typed
	for _, f := range []string{"version", "digest", "packed"} {
		require.NotNil(t, recordField(t, handle, f))
	}

	// methods typed via the metatable
	meta, ok := handle.Metatable.(*typ.Interface)
	require.True(t, ok, "handle metatable is not an interface")
	for _, name := range []string{"metadata", "entries", "resources", "fs", "close"} {
		require.NotNil(t, ifaceMethod(t, meta, name))
	}
	require.Lenf(t, meta.Methods, 5, "handle should declare exactly 5 methods, got %d", len(meta.Methods))

	// cache.list returns a typed array, not Any
	cache, ok := recordField(t, export, "cache").(*typ.Interface)
	require.True(t, ok, "hub.cache is not an interface")
	list := ifaceMethod(t, cache, "list")
	_, isArray := list.Returns[0].(*typ.Array)
	require.Truef(t, isArray, "cache.list returns %T, want a typed *typ.Array (not Any)", list.Returns[0])
}
