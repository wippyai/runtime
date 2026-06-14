// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
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
	for _, fn := range []string{"list_sources", "source", "stream"} {
		require.Equal(t, lua.LTFunction, modTbl.RawGetString(fn).Type(), "%s missing", fn)
	}
}

func TestListSourcesReturnsAllInfos(t *testing.T) {
	l := newStateWithInspector(t, &fakeInspector{
		all: []cdcapi.SourceInfo{
			{Name: "id-a", Slot: "slot_a", Publication: "pub_a", Streaming: true},
			{Name: "id-b", Slot: "slot_b", Tables: []string{"public.t"}, Failover: true},
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
			{Name: "id-a", Slot: "slot_a"},
		},
	})

	require.NoError(t, l.DoString(`
		local info, err = cdc.source("slot_a")
			assert(err == nil, "unexpected error: " .. tostring(err))
			assert(info.slot == "slot_a")

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

func TestStreamOpenAndRelease(t *testing.T) {
	l := newStateWithInspector(t, &fakeInspector{})

	require.NoError(t, l.DoString(`
		local stream, err = cdc.stream("slot_a", {
			tables = {"public.accounts"},
			ops = {"insert", "update"},
			buffer = 4,
		})
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(stream ~= nil)
		local ok, close_err = stream:release()
		assert(ok == true)
		assert(close_err == nil)
	`))
}

func TestStreamRequiresName(t *testing.T) {
	l := newStateWithInspector(t, &fakeInspector{})

	require.NoError(t, l.DoString(`
		local stream, err = cdc.stream("")
		assert(stream == nil)
		assert(err ~= nil)
	`))
}

func TestStreamRejectsInvalidBuffer(t *testing.T) {
	l := newStateWithInspector(t, &fakeInspector{})

	require.NoError(t, l.DoString(`
		local stream, err = cdc.stream("slot_a", { buffer = "bad" })
		assert(stream == nil)
		assert(err ~= nil)
	`))
}

func TestChangeHandlerUsesCanonicalLuaKeys(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	got := cdcChangeHandler(context.Background(), l, pid.Zero(), "cdc@1", []payload.Payload{
		payload.New(cdcapi.Change{
			Source:    "test:cdc",
			Op:        "insert",
			Schema:    "public",
			Table:     "accounts",
			Relation:  "public.accounts",
			LSN:       "0/16B6C50",
			CommitLSN: "0/16B6C98",
			XID:       42,
			After:     map[string]any{"email": "a@w.ai"},
		}),
	})

	tbl, ok := got.(*lua.LTable)
	require.True(t, ok)
	require.Equal(t, "test:cdc", tbl.RawGetString("source").String())
	require.Equal(t, "insert", tbl.RawGetString("op").String())
	require.Equal(t, "public.accounts", tbl.RawGetString("relation").String())
	require.Equal(t, "0/16B6C98", tbl.RawGetString("commit_lsn").String())
	require.Equal(t, lua.LNil, tbl.RawGetString("commit_lsn,omitempty"))
	require.Equal(t, lua.LNil, tbl.RawGetString("after,omitempty"))

	after, ok := tbl.RawGetString("after").(*lua.LTable)
	require.True(t, ok)
	require.Equal(t, "a@w.ai", after.RawGetString("email").String())
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
