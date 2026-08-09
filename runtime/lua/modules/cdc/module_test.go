// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/types/typ"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
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

type fakeSource struct {
	info cdcapi.SourceInfo
}

func (f *fakeSource) Info() cdcapi.SourceInfo { return f.info }

func (f *fakeSource) Subscribe(context.Context, cdcapi.StreamOptions) (cdcapi.Stream, error) {
	return nil, nil
}

type fakeRegistry struct {
	source cdcapi.Source
	all    []cdcapi.SourceInfo
}

func (f *fakeRegistry) List() []cdcapi.SourceInfo { return f.all }

func (f *fakeRegistry) Get(id registry.ID) (cdcapi.Source, bool) {
	if f.source == nil {
		return nil, false
	}
	info := f.source.Info()
	return f.source, !isZeroRegistryID(info.ID) && registryIDString(info.ID) == registryIDString(id)
}

func newStateWithRegistry(t *testing.T, registry cdcapi.Registry) *lua.LState {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)

	ctx := ctxapi.NewRootContext()
	ctx = cdcapi.WithRegistry(ctx, registry)
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
			{
				ID:         registry.NewID("test", "id-a"),
				Kind:       "db.cdc.postgres",
				State:      cdcapi.SourceStateRunning,
				Generation: "generation-a",
				Capabilities: cdcapi.Capabilities{
					Snapshot:               true,
					Durable:                true,
					Replayable:             true,
					CapturesExternalWrites: true,
					BeforeImages:           true,
				},
				Name:        "id-a",
				Slot:        "slot_a",
				Publication: "pub_a",
				Streaming:   true,
			},
			{
				ID:           registry.NewID("test", "id-b"),
				Kind:         "db.cdc.sqlite",
				State:        cdcapi.SourceStateFaulted,
				Capabilities: cdcapi.Capabilities{BeforeImages: true, Coalesced: true},
				Name:         "id-b",
				Slot:         "slot_b",
				Tables:       []string{"public.t"},
				Failover:     true,
				Faulted:      true,
			},
		},
	})

	require.NoError(t, l.DoString(`
		local rows, err = cdc.list_sources()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(#rows == 2, "expected 2 rows, got " .. tostring(#rows))
		assert(rows[1].id == "test:id-a")
		assert(rows[1].kind == "db.cdc.postgres")
		assert(rows[1].state == "running")
		assert(rows[1].generation == "generation-a")
		assert(rows[1].capabilities.snapshot == true)
		assert(rows[1].capabilities.durable == true)
		assert(rows[1].capabilities.replayable == true)
		assert(rows[1].capabilities.captures_external_writes == true)
		assert(rows[1].capabilities.before_images == true)
		assert(rows[1].slot == "slot_a")
		assert(rows[1].publication == "pub_a")
		assert(rows[1].tables == nil, "row with no tables should omit the tables key")
		assert(rows[1].streaming == true)
		assert(rows[2].slot == "slot_b")
		assert(rows[2].publication == nil, "row with no publication should omit the publication key")
		assert(rows[2].tables[1] == "public.t")
		assert(#rows[2].tables == 1)
		assert(rows[2].failover == true)
		assert(rows[2].capabilities.coalesced == true)
	`))
}

func TestListSourcesUsesDriverNeutralRegistry(t *testing.T) {
	id := registry.NewID("test", "registry-source")
	l := newStateWithRegistry(t, &fakeRegistry{
		all: []cdcapi.SourceInfo{{
			ID:    id,
			Kind:  "db.cdc.sqlite",
			State: cdcapi.SourceStateRunning,
			Name:  id.String(),
		}},
	})

	require.NoError(t, l.DoString(`
		local rows, err = cdc.list_sources()
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(#rows == 1)
		assert(rows[1].id == "test:registry-source")
		assert(rows[1].kind == "db.cdc.sqlite")
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

func TestSourceByRegistryID(t *testing.T) {
	id := registry.NewID("test", "source")
	source := &fakeSource{info: cdcapi.SourceInfo{
		ID:    id,
		Kind:  "db.cdc.sqlite",
		State: cdcapi.SourceStateRunning,
		Name:  id.String(),
	}}
	l := newStateWithRegistry(t, &fakeRegistry{source: source})

	require.NoError(t, l.DoString(`
		local info, err = cdc.source("test:source")
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(info ~= nil)
		assert(info.id == "test:source")
		assert(info.kind == "db.cdc.sqlite")
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
			max_bytes = 4096,
			snapshot = true,
			after = "cursor-1",
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

func TestStreamRejectsMalformedOptions(t *testing.T) {
	cases := map[string]string{
		"tables type":          `{ tables = "accounts" }`,
		"tables element":       `{ tables = { 1 } }`,
		"ops element":          `{ ops = { "insert", 2 } }`,
		"fractional buffer":    `{ buffer = 1.5 }`,
		"zero buffer":          `{ buffer = 0 }`,
		"oversized buffer":     `{ buffer = 65537 }`,
		"max bytes type":       `{ max_bytes = "4096" }`,
		"fractional max bytes": `{ max_bytes = 1.5 }`,
		"zero max bytes":       `{ max_bytes = 0 }`,
		"negative max bytes":   `{ max_bytes = -1 }`,
		"infinite max bytes":   `{ max_bytes = math.huge }`,
		"snapshot type":        `{ snapshot = "true" }`,
		"after type":           `{ after = 42 }`,
		"empty after":          `{ after = "" }`,
		"whitespace after":     `{ after = " \t\n" }`,
		"unknown field":        `{ unsupported = true }`,
		"numeric field":        `{ [1] = "unsupported" }`,
	}
	for name, options := range cases {
		t.Run(name, func(t *testing.T) {
			l := newStateWithInspector(t, &fakeInspector{})
			script := `
				local stream, err = cdc.stream("source", ` + options + `)
				assert(stream == nil)
				assert(err ~= nil)
			`
			require.NoError(t, l.DoString(script))
		})
	}
}

func TestStreamMaxBytesPreservesIntegerAndRejectsInexactFloat(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	integerOptions := l.CreateTable(0, 1)
	integerOptions.RawSetString("max_bytes", lua.LInteger(1<<63-1))
	l.Push(integerOptions)
	options, luaErr := streamOptionsFromLua(l, 1)
	require.Nil(t, luaErr)
	require.Equal(t, int64(1<<63-1), options.MaxBytes)
	l.SetTop(0)

	floatOptions := l.CreateTable(0, 1)
	floatOptions.RawSetString("max_bytes", lua.LNumber(1<<53))
	l.Push(floatOptions)
	_, luaErr = streamOptionsFromLua(l, 1)
	require.NotNil(t, luaErr)
}

func TestStringArrayFieldRejectsOutOfRangeIndex(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	values := l.CreateTable(0, 1)
	values.RawSetInt(maxStreamItems+1, lua.LString("out-of-range"))
	options := l.CreateTable(0, 1)
	options.RawSetString("tables", values)
	_, errMsg := stringArrayField(options, "tables")
	require.NotEmpty(t, errMsg)
}

func TestStreamBufferCapacityIsBounded(t *testing.T) {
	for _, test := range []struct {
		name   string
		input  int
		wanted int
	}{
		{name: "default", input: 0, wanted: defaultStreamBuffer},
		{name: "negative uses default", input: -1, wanted: defaultStreamBuffer},
		{name: "configured", input: 128, wanted: 128},
		{name: "maximum", input: maxStreamItems, wanted: maxStreamItems},
		{name: "internal overflow is capped", input: maxStreamItems + 1, wanted: maxStreamItems},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.wanted, streamBufferCapacity(test.input))
		})
	}
}

func TestModuleTypesUseIntegerBufferAndTypedChannel(t *testing.T) {
	manifest := ModuleTypes()
	streamOptions, ok := manifest.LookupType("StreamOptions")
	require.True(t, ok)
	optionsRecord, ok := streamOptions.(*typ.Record)
	require.True(t, ok)
	require.Equal(t, typ.Integer, optionsRecord.GetField("buffer").Type)
	require.Equal(t, typ.Integer, optionsRecord.GetField("max_bytes").Type)
	require.NotEqual(t, typ.Any, cdcChannelType)
}

func TestChangeHandlerUsesCanonicalLuaKeys(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	got := cdcChangeHandler(context.Background(), l, pid.Zero(), "cdc@1", []payload.Payload{
		payload.New(cdcapi.Change{
			SourceID:    registry.NewID("test", "cdc"),
			Source:      "test:cdc",
			Op:          "insert",
			Schema:      "public",
			Table:       "accounts",
			Relation:    "public.accounts",
			LSN:         "0/16B6C50",
			CommitLSN:   "0/16B6C98",
			Cursor:      "cursor-1",
			Generation:  "generation-1",
			Transaction: "transaction-1",
			XID:         42,
			After:       map[string]any{"email": "a@w.ai"},
		}),
	})

	tbl, ok := got.(*lua.LTable)
	require.True(t, ok)
	require.Equal(t, "test:cdc", tbl.RawGetString("source").String())
	require.Equal(t, "test:cdc", tbl.RawGetString("source_id").String())
	require.Equal(t, "insert", tbl.RawGetString("op").String())
	require.Equal(t, "public.accounts", tbl.RawGetString("relation").String())
	require.Equal(t, "0/16B6C98", tbl.RawGetString("commit_lsn").String())
	require.Equal(t, "cursor-1", tbl.RawGetString("cursor").String())
	require.Equal(t, "generation-1", tbl.RawGetString("generation").String())
	require.Equal(t, "transaction-1", tbl.RawGetString("transaction").String())
	require.Equal(t, lua.LNil, tbl.RawGetString("commit_lsn,omitempty"))
	require.Equal(t, lua.LNil, tbl.RawGetString("after,omitempty"))

	after, ok := tbl.RawGetString("after").(*lua.LTable)
	require.True(t, ok)
	require.Equal(t, "a@w.ai", after.RawGetString("email").String())
}

func TestChangeHandlerConvertsTypedStreamError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	got := cdcChangeHandler(context.Background(), l, pid.Zero(), "cdc@1", []payload.Payload{
		payload.NewError(errors.New("capture gap")),
	})
	streamErr, ok := lua.AsError(got)
	require.True(t, ok)
	require.Contains(t, streamErr.Error(), "capture gap")
}

func TestModuleTypesMatchRuntimeFields(t *testing.T) {
	manifest := ModuleTypes()

	assertRecordFields := func(name string, want []string) {
		t.Helper()
		value, ok := manifest.LookupType(name)
		require.True(t, ok, "%s type is not defined", name)
		record, ok := value.(*typ.Record)
		require.True(t, ok, "%s is %T, want record", name, value)
		for _, field := range want {
			require.NotNil(t, record.GetField(field), "%s.%s is missing", name, field)
		}
	}

	assertRecordFields("Capabilities", []string{
		"snapshot", "durable", "replayable", "captures_external_writes", "before_images", "coalesced",
	})
	assertRecordFields("SourceInfo", []string{
		"id", "kind", "state", "generation", "capabilities", "name", "slot", "publication",
		"engine", "file", "db_resource", "epoch", "error", "tables", "streaming", "failover",
		"temporary", "snapshot", "faulted",
	})
	assertRecordFields("StreamOptions", []string{"tables", "ops", "buffer", "max_bytes", "snapshot", "after"})
	assertRecordFields("Change", []string{
		"source_id", "source", "op", "schema", "table", "relation", "lsn", "commit_lsn", "cursor",
		"generation", "transaction", "error", "xid", "before", "after",
	})
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
