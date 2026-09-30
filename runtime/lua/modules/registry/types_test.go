// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/types/typ"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
)

// Compare real Lua values to the public manifest, including undeclared fields.
// This catches drift on nested read records that a type-check-only test misses.
func registryShapeError(l *lua.LState, declared typ.Type, actual lua.LValue) error {
	switch shape := declared.(type) {
	case *typ.Optional:
		if actual == lua.LNil {
			return nil
		}
		return registryShapeError(l, shape.Inner, actual)
	case *typ.Union:
		for _, member := range shape.Members {
			if registryShapeError(l, member, actual) == nil {
				return nil
			}
		}
	case *typ.Record:
		table, ok := actual.(*lua.LTable)
		if !ok {
			break
		}
		for _, field := range shape.Fields {
			v := table.RawGetString(field.Name)
			if v == lua.LNil && field.Optional {
				continue
			}
			if v == lua.LNil {
				return fmt.Errorf("missing required field %s", field.Name)
			}
			if err := registryShapeError(l, field.Type, v); err != nil {
				return fmt.Errorf("%s: %w", field.Name, err)
			}
		}
		var err error
		table.ForEach(func(k, v lua.LValue) {
			if shape.GetField(k.String()) == nil {
				err = fmt.Errorf("undeclared field %s", k)
			}
		})
		return err
	case *typ.Array:
		table, ok := actual.(*lua.LTable)
		if !ok {
			break
		}
		var err error
		table.ForEach(func(k, v lua.LValue) {
			if err == nil {
				err = registryShapeError(l, shape.Element, v)
			}
		})
		return err
	case *typ.Map:
		table, ok := actual.(*lua.LTable)
		if !ok {
			break
		}
		var err error
		table.ForEach(func(k, v lua.LValue) {
			if err == nil {
				err = registryShapeError(l, shape.Key, k)
			}
			if err == nil {
				err = registryShapeError(l, shape.Value, v)
			}
		})
		return err
	case *typ.Interface:
		if actual.Type() != lua.LTUserData {
			break
		}
		for _, method := range shape.Methods {
			member, ok := value.GetField(l, actual, method.Name)
			if !ok || member.Type() != lua.LTFunction {
				return fmt.Errorf("missing method %s", method.Name)
			}
		}
		return nil
	default:
		switch {
		case typ.TypeEquals(declared, typ.Any):
			return nil
		case typ.TypeEquals(declared, typ.String) && actual.Type() == lua.LTString:
			return nil
		case typ.TypeEquals(declared, typ.Boolean) && actual.Type() == lua.LTBool:
			return nil
		case typ.TypeEquals(declared, typ.Number) && (actual.Type() == lua.LTNumber || actual.Type() == lua.LTInteger):
			return nil
		case typ.TypeEquals(declared, typ.Self) && actual.Type() == lua.LTUserData:
			return nil
		}
	}
	return fmt.Errorf("declared %s, returned %s", declared, actual.Type())
}

func registryManifestMethod(t *testing.T, group, method string) *typ.Function {
	t.Helper()
	manifest := ModuleTypes()
	iface := manifest.Export.(*typ.Interface)
	if group != "registry" {
		declared, ok := manifest.LookupType(group)
		require.True(t, ok)
		iface = declared.(*typ.Interface)
	}
	for _, m := range iface.Methods {
		if m.Name == method {
			return m.Type
		}
	}
	t.Fatalf("manifest lacks %s.%s", group, method)
	return nil
}

func TestRegistryManifestMatchesReturnedLuaShapes(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	ctx := setupContextWithTranscoder()
	hist := historymem.New()
	v1 := version.FromParent(version.New(0), 1)
	resolution := testResolution("2.0.0")
	resolution.Roots = []regapi.DependencyRoot{{ID: "app:root", Component: "org/app", Version: "*"}}
	resolution.References = []regapi.DependencyRoot{{ID: "app:ref", Component: "org/app", Version: "*"}}
	resolution.Modules[0].VersionID = "exact-id"
	resolution.Modules[0].Source = "hub"
	resolution.Modules[0].SizeBytes = 42
	resolution.Modules[0].Protected = true
	resolution = resolution.Canonical()
	entry := regapi.Entry{ID: regapi.NewID("app", "visible"), Kind: "registry.entry", Registry: regapi.EntryMetadata{Owner: "org/app", Root: true}}
	require.NoError(t, hist.SaveWithDependencyResolution(v1, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: entry}}, resolution, true))
	reg := &resolutionTestRegistry{mockRegistry: planTestMock(), hist: hist, resolution: resolution}
	reg.entries[entry.ID.String()] = entry
	reg.snapshot = regapi.Snapshot{Version: v1, Entries: regapi.State{entry}, Registry: regapi.StateMetadata{Resolution: resolution}}
	l.SetContext(regapi.WithRegistry(ctx, reg))
	lua.OpenErrors(l)
	setupModule(l)
	l.SetGlobal("check", l.NewFunction(func(l *lua.LState) int {
		method := registryManifestMethod(t, l.CheckString(1), l.CheckString(2))
		require.LessOrEqual(t, l.GetTop()-2, len(method.Returns), "extra return values")
		for i, declared := range method.Returns {
			actual := l.Get(i + 3)
			// Lua fills absent trailing error returns with nil.
			require.NoError(t, registryShapeError(l, declared, actual), "%s.%s return %d", l.Get(1), l.Get(2), i+1)
		}
		return 0
	}))
	l.SetGlobal("check_input", l.NewFunction(func(l *lua.LState) int {
		method := registryManifestMethod(t, l.CheckString(1), l.CheckString(2))
		offset := 0
		if len(method.Params) > 0 && method.Params[0].Name == "self" {
			offset = 1
		}
		require.Equal(t, len(method.Params)-offset, l.GetTop()-2)
		for i, p := range method.Params[offset:] {
			require.NoError(t, registryShapeError(l, p.Type, l.Get(i+3)), "parameter %s", p.Name)
		}
		return 0
	}))
	require.NoError(t, l.DoString(`
  check("registry", "get", registry.get("app:visible"))
  check("registry", "parse_id", registry.parse_id("app:visible"))
  check("registry", "snapshot", registry.snapshot())
  check("registry", "snapshot_at", registry.snapshot_at(1))
  check("registry", "current_version", registry.current_version())
  check("registry", "versions", registry.versions())
  check("registry", "history", registry.history())
  local snap = assert(registry.snapshot())
  check("Snapshot", "entries", snap:entries())
  check("Snapshot", "get", snap:get("app:visible"))
  check("Snapshot", "namespace", snap:namespace("app"))
  check("Snapshot", "find", snap:find({[".ns"]="app"}))
  check("Snapshot", "state", snap:state())
  check("Snapshot", "changes", snap:changes())
  check("Snapshot", "version", snap:version())
  local hist = assert(registry.history())
  check("History", "versions", hist:versions())
  check("History", "get_version", hist:get_version(1))
  check("History", "snapshot_at", hist:snapshot_at(assert(hist:get_version(1))))
  local v = snap:version()
  check("Version", "id", v:id())
  check("Version", "string", v:string())
  check("Version", "previous", v:previous())
  check("Version", "next", v:next())
  local changes = snap:changes()
  local write = {id={ns="app",name="root"},kind="ns.dependency",dependency_root=true}
  check_input("Changes", "create", write)
  check("Changes", "create", changes:create(write))
  check_input("Changes", "update", {id="app:visible",kind="registry.entry",data={value=1}})
  check("Changes", "update", changes:update({id="app:visible",kind="registry.entry",data={value=1}}))
  check("Changes", "delete", changes:delete("app:old"))
  check("Changes", "ops", changes:ops())
  check("Changes", "plan", changes:plan())
  check("Changes", "apply", changes:apply())
  local from = {{id="app:old",kind="registry.entry"}, {id="app:visible",kind="registry.entry",data={value=1}}}
  local to = {{id="app:visible",kind="registry.entry",data={value=2}},write}
  check_input("registry", "build_delta", from,to)
  check("registry", "build_delta", registry.build_delta(from,to))
 `))
	read, ok := ModuleTypes().LookupType("Entry")
	require.True(t, ok)
	require.Nil(t, read.(*typ.Record).GetField("dependency_root"))
	require.Nil(t, read.(*typ.Record).GetField("registry"))
	input := registryManifestMethod(t, "Changes", "create").Params[1].Type.(*typ.Record)
	require.True(t, input.GetField("dependency_root").Optional)
	// Absent graph and module optional fields must also match the manifest.
	reg.snapshot.Registry.Resolution = nil
	require.NoError(t, l.DoString(`check("Snapshot","state",registry.snapshot():state())`))
	reg.resolution = &regapi.DependencyResolution{Modules: []regapi.ResolvedModule{{Name: "org/app", Version: "1.0.0"}}}
	require.NoError(t, l.DoString(`check("Changes","plan",registry.snapshot():changes():create({id="app:new",kind="registry.entry"}):plan())`))
}
