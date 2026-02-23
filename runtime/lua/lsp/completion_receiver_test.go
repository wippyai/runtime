// SPDX-License-Identifier: MPL-2.0

package lsp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	"github.com/wippyai/go-lua/types/typ/unwrap"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/lsp/indexing"
)

type testProvider struct {
	deps        map[string]*io.Manifest
	builtinHash string
	modules     []*luaapi.ModuleDef
}

func (p *testProvider) AllNodes() []indexing.NodeInfo {
	return nil
}

func (p *testProvider) Node(id registry.ID) (indexing.NodeInfo, error) {
	return indexing.NodeInfo{ID: id}, nil
}

func (p *testProvider) DirectDependencies(id registry.ID) ([]registry.ID, error) {
	return nil, nil
}

func (p *testProvider) DependencyManifests(id registry.ID) map[string]*io.Manifest {
	return p.deps
}

func (p *testProvider) ModuleDefs() []*luaapi.ModuleDef {
	return p.modules
}

func (p *testProvider) BuiltinManifestHash() string {
	return p.builtinHash
}

func cursorPosition(t *testing.T, src string) (string, int, int) {
	t.Helper()
	idx := strings.Index(src, "|")
	require.NotEqual(t, -1, idx, "cursor marker not found")
	line, col := 1, 1
	for i := 0; i < idx; i++ {
		if src[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	clean := src[:idx] + src[idx+1:]
	return clean, line, col
}

func assertRecordHasField(t *testing.T, got typ.Type, name string) {
	t.Helper()
	require.NotNil(t, got)
	got = unwrap.Underlying(got)
	rec, ok := got.(*typ.Record)
	require.True(t, ok, "expected record type, got %T", got)
	for _, field := range rec.Fields {
		if field.Name == name {
			return
		}
	}
	require.Failf(t, "missing field", "field %q not found", name)
}

func newTestServiceWithProvider(provider indexing.Provider) *Service {
	svc := &Service{
		provider:  provider,
		documents: indexing.NewDocumentStore(),
	}
	svc.globalTypes = buildGlobalTypes(provider)
	return svc
}

func TestResolveReceiverTypeAt_Expressions(t *testing.T) {
	rec := typ.NewRecord().Field("pid", typ.Number).Field("name", typ.String).Build()
	manifest := &io.Manifest{
		Path:    "test",
		Version: 1,
		Globals: map[string]typ.Type{
			"process":    rec,
			"getProcess": typ.Func().Returns(rec).Build(),
			"arr":        typ.NewArray(rec),
		},
	}
	provider := &testProvider{
		modules: []*luaapi.ModuleDef{{
			Name:  "test",
			Types: func() *io.Manifest { return manifest },
		}},
		builtinHash: "builtin-v1",
	}
	svc := newTestServiceWithProvider(provider)

	id := registry.NewID("app", "completion")
	fileID := id.String()

	cases := []struct {
		name string
		src  string
	}{
		{
			name: "global",
			src:  "local p = process.|",
		},
		{
			name: "call",
			src:  "local p = getProcess().|",
		},
		{
			name: "index",
			src:  "local p = arr[1].|",
		},
		{
			name: "logical",
			src:  "local x = process\nlocal y = process\nlocal p = (x or y).|",
		},
		{
			name: "colon",
			src:  "local p = process:|",
		},
		{
			name: "long_ident_colon",
			src:  "local snapshot = process\nsnapshot:|",
		},
		{
			name: "colon_with_space",
			src:  "local snapshot = process\nsnapshot: |",
		},
	}

	version := 1
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source, line, col := cursorPosition(t, tc.src)
			svc.documents.Set(id, source, version)
			version++
			got := svc.ResolveReceiverTypeAt(fileID, line, col)
			assertRecordHasField(t, got, "pid")
		})
	}
}

func TestResolveReceiverTypeAt_BalancesBlocks(t *testing.T) {
	rec := typ.NewRecord().Field("pid", typ.Number).Build()
	manifest := &io.Manifest{
		Path:    "test",
		Version: 1,
		Globals: map[string]typ.Type{
			"getProcess": typ.Func().Returns(rec).Build(),
		},
	}
	provider := &testProvider{
		modules: []*luaapi.ModuleDef{{
			Name:  "test",
			Types: func() *io.Manifest { return manifest },
		}},
		builtinHash: "builtin-v1",
	}
	svc := newTestServiceWithProvider(provider)

	source, line, col := cursorPosition(t, "local function demo()\n  if true then\n    getProcess().|\n")
	id := registry.NewID("app", "balance")
	fileID := id.String()
	svc.documents.Set(id, source, 1)

	member := memberLocationAt(source, line, col)
	_, _, err := parseCompletionSource(source, fileID, line, col, member)
	require.NoError(t, err)

	got := svc.ResolveReceiverTypeAt(fileID, line, col)
	assertRecordHasField(t, got, "pid")
}

func TestResolveReceiverTypeAt_RebuildsOnDepChange(t *testing.T) {
	rec := typ.NewRecord().Field("pid", typ.Number).Build()
	manifest := &io.Manifest{
		Path:    "test",
		Version: 1,
		Globals: map[string]typ.Type{
			"process": rec,
		},
	}
	dep := &io.Manifest{Path: "dep", Version: 1}
	provider := &testProvider{
		modules: []*luaapi.ModuleDef{{
			Name:  "test",
			Types: func() *io.Manifest { return manifest },
		}},
		deps:        map[string]*io.Manifest{"dep": dep},
		builtinHash: "builtin-v1",
	}
	svc := newTestServiceWithProvider(provider)

	source, line, col := cursorPosition(t, "local p = process.|")
	id := registry.NewID("app", "deps")
	fileID := id.String()
	svc.documents.Set(id, source, 1)

	_ = svc.ResolveReceiverTypeAt(fileID, line, col)
	first := svc.completionChecker
	require.NotNil(t, first)

	dep.Version = 2
	_ = svc.ResolveReceiverTypeAt(fileID, line, col)
	second := svc.completionChecker
	require.NotNil(t, second)
	require.NotSame(t, first, second)
}
