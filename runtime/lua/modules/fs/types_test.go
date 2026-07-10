// SPDX-License-Identifier: MPL-2.0

package fs

import (
	"testing"

	"github.com/wippyai/go-lua/types/typ"
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

	iterator, ok := readdir.Returns[0].(*typ.Function)
	if !ok {
		t.Fatalf("readdir iterator = %T, want function", readdir.Returns[0])
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
	for _, field := range []string{"name", "type"} {
		got := record.GetField(field)
		if got == nil || !typ.TypeEquals(got.Type, typ.String) {
			t.Fatalf("entry.%s = %v, want string", field, got)
		}
	}
}
