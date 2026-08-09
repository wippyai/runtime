// SPDX-License-Identifier: MPL-2.0

package fs

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	"github.com/wippyai/runtime/runtime/lua/code"
)

func TestReaddirTypeReturnsDirectoryEntryIterator(t *testing.T) {
	manifest := ModuleTypes()
	fs, ok := manifest.LookupType("FS")
	if !ok {
		t.Fatal("FS type is not defined")
	}

	methods := fs.(*typ.Interface).Methods
	var readdir *typ.Function
	for _, method := range methods {
		if method.Name == "readdir" {
			readdir = method.Type
			break
		}
	}
	if readdir == nil {
		t.Fatal("readdir method is not defined")
	}
	if len(readdir.Returns) != 2 {
		t.Fatalf("readdir returns %d values, want iterator and state", len(readdir.Returns))
	}

	optionalIterator, ok := readdir.Returns[0].(*typ.Optional)
	if !ok {
		t.Fatalf("readdir iterator = %T, want optional function", readdir.Returns[0])
	}
	iterator, ok := optionalIterator.Inner.(*typ.Function)
	if !ok {
		t.Fatalf("readdir iterator = %T, want function", optionalIterator.Inner)
	}
	if len(iterator.Params) != 2 || iterator.Params[0].Name != "state" || iterator.Params[0].Optional || !iterator.Params[1].Optional {
		t.Fatalf("iterator params = %+v, want required state and optional control", iterator.Params)
	}
	if len(iterator.Returns) != 1 {
		t.Fatalf("iterator returns %d values, want directory entry", len(iterator.Returns))
	}
	entry, ok := iterator.Returns[0].(*typ.Optional)
	if !ok {
		t.Fatalf("iterator entry = %T, want optional record", iterator.Returns[0])
	}
	record, ok := entry.Inner.(*typ.Record)
	if !ok {
		t.Fatalf("iterator entry = %T, want record", entry.Inner)
	}
	name := record.GetField("name")
	if name == nil || !typ.TypeEquals(name.Type, typ.String) {
		t.Fatalf("entry.name = %v, want string", name)
	}
	kind := record.GetField("type")
	wantKind := typ.NewUnion(typ.LiteralString(typeFile), typ.LiteralString(typeDir))
	if kind == nil || !typ.TypeEquals(kind.Type, wantKind) {
		t.Fatalf("entry.type = %v, want %s", kind, wantKind)
	}

	wantStateOrError := typ.NewUnion(dirIteratorStateType, typ.LuaError)
	if !typ.TypeEquals(readdir.Returns[1], wantStateOrError) {
		t.Fatalf("readdir state/error = %s, want %s", readdir.Returns[1], wantStateOrError)
	}
}

func TestReaddirTypeChecksGenericForProtocol(t *testing.T) {
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)

	_, diagnostics, err := tc.Check(`
local fs = require("fs")
local volume = fs.get("app:temp")
local iterator, state = volume:readdir("/")
if iterator then
    for entry in iterator, state do
        local name: string = entry.name
        local kind: "file" | "directory" = entry.type
    end
end
`, "fs_readdir_types.lua", map[string]*io.Manifest{"fs": ModuleTypes()})
	require.NoError(t, err)
	require.False(t, code.HasErrors(diagnostics), "unexpected diagnostics: %v", diagnostics)
}
