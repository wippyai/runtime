// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
)

type fakeInspector struct {
	all []cdcapi.SourceInfo
}

func (f *fakeInspector) List() []cdcapi.SourceInfo {
	return f.all
}

func (f *fakeInspector) Get(name string) (cdcapi.SourceInfo, bool) {
	for _, info := range f.all {
		if info.Slot == name || info.Name == name {
			return info, true
		}
	}
	return cdcapi.SourceInfo{}, false
}

func newStateWithInspector(t *testing.T, inspector cdcapi.SourceInspector) *lua.LState {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)

	ctx := cdcapi.WithSourceInspector(context.Background(), inspector)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
	return l
}

func TestModuleLoad(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	mod := l.GetGlobal("cdc")
	require.Equal(t, lua.LTTable, mod.Type())
	modTbl := mod.(*lua.LTable)
	for _, fn := range []string{"list_sources", "source"} {
		require.Equal(t, lua.LTFunction, modTbl.RawGetString(fn).Type(), "%s missing", fn)
	}
}

func TestListSourcesReturnsAllInfos(t *testing.T) {
	l := newStateWithInspector(t, &fakeInspector{
		all: []cdcapi.SourceInfo{
			{Name: "id-a", Slot: "slot_a", EventSystem: "postgres_cdc", Publication: "pub_a", Streaming: true},
			{Name: "id-b", Slot: "slot_b", EventSystem: "postgres_cdc", Tables: []string{"public.t"}, Failover: true},
		},
	})

	require.NoError(t, l.DoString(`
		local rows, err = cdc.list_sources()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(#rows == 2, "expected 2 rows, got " .. tostring(#rows))
		assert(rows[1].slot == "slot_a")
		assert(rows[1].publication == "pub_a")
		assert(rows[1].tables == nil, "row with no tables should omit the tables key")
		assert(rows[1].streaming == true)
		assert(rows[2].slot == "slot_b")
		assert(rows[2].publication == nil, "row with no publication should omit the publication key")
		assert(rows[2].tables[1] == "public.t")
		assert(#rows[2].tables == 1)
		assert(rows[2].failover == true)
	`))
}

func TestSourceByName(t *testing.T) {
	l := newStateWithInspector(t, &fakeInspector{
		all: []cdcapi.SourceInfo{
			{Name: "id-a", Slot: "slot_a", EventSystem: "postgres_cdc"},
		},
	})

	require.NoError(t, l.DoString(`
		local info, err = cdc.source("slot_a")
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(info.slot == "slot_a")
		assert(info.event_system == "postgres_cdc")

		local missing, err2 = cdc.source("nope")
		assert(err2 == nil)
		assert(missing == nil, "expected nil for unknown source")
	`))
}

func TestSourceRequiresName(t *testing.T) {
	l := newStateWithInspector(t, &fakeInspector{})

	err := l.DoString(`
		local info, err = cdc.source("")
		if err == nil then error("expected error for empty name") end
	`)
	require.NoError(t, err)
}

func TestListSourcesFailsWithoutInspector(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	require.NoError(t, l.DoString(`
		local rows, err = cdc.list_sources()
		assert(rows == nil)
		assert(err ~= nil)
	`))
}
