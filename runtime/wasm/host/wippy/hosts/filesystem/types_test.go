// SPDX-License-Identifier: MPL-2.0

package filesystem

import (
	"context"
	stdio "io"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"

	fsapi "github.com/wippyai/runtime/api/fs"
	streamhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/io"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type testFilesystem struct {
	fsapi.FS
	openFileFn  func(string, int, fs.FileMode) (fsapi.File, error)
	renameCalls int
}

func (f *testFilesystem) OpenFile(name string, flag int, perm fs.FileMode) (fsapi.File, error) {
	if f.openFileFn != nil {
		return f.openFileFn(name, flag, perm)
	}
	return f.FS.OpenFile(name, flag, perm)
}

func (f *testFilesystem) Rename(oldName, newName string) error {
	f.renameCalls++
	return f.FS.Rename(oldName, newName)
}

type testFile struct {
	fsapi.File
	readData    []byte
	readErr     error
	seekOffsets []int64
	seekWhences []int
	writes      [][]byte
	closeCalls  int
}

func (f *testFile) Read(p []byte) (int, error) {
	n := copy(p, f.readData)
	return n, f.readErr
}

func (f *testFile) Write(p []byte) (int, error) {
	f.writes = append(f.writes, append([]byte(nil), p...))
	return len(p), nil
}

func (f *testFile) Seek(offset int64, whence int) (int64, error) {
	f.seekOffsets = append(f.seekOffsets, offset)
	f.seekWhences = append(f.seekWhences, whence)
	return offset, nil
}

func (f *testFile) Sync() error { return nil }
func (f *testFile) Close() error {
	f.closeCalls++
	return nil
}

func TestR07DescriptorReadPreservesPartialEOF(t *testing.T) {
	file := &testFile{readData: []byte("part"), readErr: stdio.EOF}
	filesystem := &testFilesystem{openFileFn: func(name string, flag int, perm fs.FileMode) (fsapi.File, error) {
		if name != "item" || flag != os.O_RDONLY || perm != 0 {
			t.Fatalf("OpenFile(%q, %d, %v), want item opened read-only", name, flag, perm)
		}
		return file, nil
	}}
	resources := preview2.NewResourceTable()
	handle := resources.Add(newDescriptorResource(filesystem, "item", false, true))

	got, gotErr := NewTypesHost(resources).MethodDescriptorRead(context.Background(), handle, 16, 0)
	if gotErr != nil {
		t.Fatalf("descriptor read error = %v", gotErr)
	}
	if len(got) != 2 {
		t.Fatalf("descriptor read result = %#v, want two tuple fields", got)
	}
	data, ok := got[0].([]byte)
	if !ok || string(data) != "part" {
		t.Fatalf("descriptor read data = %#v, want part", got[0])
	}
	eof, ok := got[1].(bool)
	if !ok || !eof {
		t.Fatalf("descriptor read EOF = %#v, want true", got[1])
	}
}

func TestR08DescriptorBoundedAllocationCalculation(t *testing.T) {
	if got, ok := boundedAllocationSize(37); !ok || got != 37 {
		t.Fatalf("boundedAllocationSize(37) = (%d, %v), want (37, true)", got, ok)
	}
	if got, ok := boundedAllocationSize(preview2.MaxAllocationSize); !ok || got != preview2.MaxAllocationSize {
		t.Fatalf("boundedAllocationSize(max) = (%d, %v), want (%d, true)", got, ok, preview2.MaxAllocationSize)
	}
	if got, ok := boundedAllocationSize(preview2.MaxAllocationSize + 1); ok || got != 0 {
		t.Fatalf("boundedAllocationSize(max+1) = (%d, %v), want (0, false)", got, ok)
	}
}

func TestR09WriteViaStreamOffsetAndClose(t *testing.T) {
	file := &testFile{}
	filesystem := &testFilesystem{openFileFn: func(name string, flag int, perm fs.FileMode) (fsapi.File, error) {
		if name != "item" || flag != os.O_WRONLY|os.O_CREATE || perm != 0644 {
			t.Fatalf("OpenFile(%q, %d, %v), want writable item", name, flag, perm)
		}
		return file, nil
	}}
	resources := preview2.NewResourceTable()
	descriptor := resources.Add(newDescriptorResource(filesystem, "item", false, false))
	stream, gotErr := NewTypesHost(resources).MethodDescriptorWriteViaStream(context.Background(), descriptor, 11)
	if gotErr != nil {
		t.Fatalf("write-via-stream error = %v", gotErr)
	}

	streams := streamhost.NewStreamsHost(resources)
	if writeErr := streams.MethodOutputStreamWrite(context.Background(), stream, []byte("abc")); writeErr != nil {
		t.Fatalf("stream write error = %v", writeErr)
	}
	streams.ResourceDropOutputStream(context.Background(), stream)
	streams.ResourceDropOutputStream(context.Background(), stream)

	if len(file.seekOffsets) != 1 || file.seekOffsets[0] != 11 || file.seekWhences[0] != 0 {
		t.Fatalf("Seek calls = offsets:%v whences:%v, want offset 11 from start once", file.seekOffsets, file.seekWhences)
	}
	if len(file.writes) != 1 || string(file.writes[0]) != "abc" {
		t.Fatalf("Write calls = %#v, want abc once", file.writes)
	}
	if file.closeCalls != 1 {
		t.Fatalf("Close calls = %d, want 1", file.closeCalls)
	}
}

func TestR10DirectoryStreamTypedExhaustion(t *testing.T) {
	filesystem := fsapi.NewReadOnlyFS(fstest.MapFS{
		"file.txt": {Data: []byte("data")},
		"folder":   {Mode: fs.ModeDir},
	})
	resources := preview2.NewResourceTable()
	descriptor := resources.Add(newDescriptorResource(filesystem, ".", true, true))
	host := NewTypesHost(resources)
	stream, gotErr := host.MethodDescriptorReadDirectory(context.Background(), descriptor)
	if gotErr != nil {
		t.Fatalf("read-directory error = %v", gotErr)
	}

	gotTypes := make(map[string]uint8)
	for i := 0; i < 2; i++ {
		entry, readErr := host.MethodDirectoryEntryStreamReadDirectoryEntry(context.Background(), stream)
		if readErr != nil || entry == nil {
			t.Fatalf("entry %d = %#v, error %v", i, entry, readErr)
		}
		gotTypes[entry.Name] = entry.Type
	}
	if gotTypes["file.txt"] != uint8(DescriptorTypeRegularFile) || gotTypes["folder"] != uint8(DescriptorTypeDirectory) {
		t.Fatalf("entry types = %#v, want literal regular file and directory types", gotTypes)
	}
	entry, readErr := host.MethodDirectoryEntryStreamReadDirectoryEntry(context.Background(), stream)
	if readErr != nil || entry != nil {
		t.Fatalf("exhausted entry = %#v, error %v; want nil, nil", entry, readErr)
	}
}

func TestR11RenameRejectsReadonlyDestination(t *testing.T) {
	filesystem := &testFilesystem{FS: fsapi.NewReadOnlyFS(fstest.MapFS{
		"source.txt": {Data: []byte("source")},
	})}
	resources := preview2.NewResourceTable()
	source := resources.Add(newDescriptorResource(filesystem, ".", true, false))
	destination := resources.Add(newDescriptorResource(filesystem, ".", true, true))

	gotErr := NewTypesHost(resources).MethodDescriptorRenameAt(context.Background(), source, "source.txt", destination, "moved.txt")
	if gotErr == nil || gotErr.Code != ErrorReadOnly {
		t.Fatalf("rename error = %#v, want read-only", gotErr)
	}
	if filesystem.renameCalls != 0 {
		t.Fatalf("Rename calls = %d, want 0", filesystem.renameCalls)
	}
	if _, statErr := filesystem.Stat("source.txt"); statErr != nil {
		t.Fatalf("source missing after rejected rename: %v", statErr)
	}
}
