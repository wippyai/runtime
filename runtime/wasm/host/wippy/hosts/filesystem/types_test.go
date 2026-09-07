// SPDX-License-Identifier: MPL-2.0

package filesystem

import (
	"context"
	stdio "io"
	"io/fs"
	"math"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	fsapi "github.com/wippyai/runtime/api/fs"
	streamhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/io"
	directoryfs "github.com/wippyai/runtime/service/fs/directory"
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
	closeCalls  atomic.Int32
}

type testFileInfo struct{ size int64 }

func (testFileInfo) Name() string       { return "item" }
func (i testFileInfo) Size() int64      { return i.size }
func (testFileInfo) Mode() fs.FileMode  { return 0 }
func (testFileInfo) ModTime() time.Time { return time.Time{} }
func (testFileInfo) IsDir() bool        { return false }
func (testFileInfo) Sys() any           { return nil }

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
func (f *testFile) Stat() (fs.FileInfo, error) {
	return testFileInfo{size: int64(len(f.readData))}, nil
}
func (f *testFile) Close() error {
	f.closeCalls.Add(1)
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

func TestDescriptorAppendRequiresAtomicCapability(t *testing.T) {
	file := &testFile{}
	filesystem := &testFilesystem{openFileFn: func(string, int, fs.FileMode) (fsapi.File, error) { return file, nil }}
	resources := preview2.NewResourceTable()
	t.Cleanup(func() { _ = resources.Close() })
	handle := resources.Add(newDescriptorResource(filesystem, "item", false, false))
	if _, got := NewTypesHost(resources).MethodDescriptorAppendViaStream(context.Background(), handle); got == nil || got.Code != ErrorUnsupported {
		t.Fatalf("append without atomic provider error = %#v, want unsupported", got)
	}
}

func TestDescriptorSetSizeRejectsInt64Overflow(t *testing.T) {
	file := &testFile{}
	filesystem := &testFilesystem{openFileFn: func(string, int, fs.FileMode) (fsapi.File, error) { return file, nil }}
	resources := preview2.NewResourceTable()
	t.Cleanup(func() { _ = resources.Close() })
	handle := resources.Add(newDescriptorResource(filesystem, "item", false, false))
	if got := NewTypesHost(resources).MethodDescriptorSetSize(context.Background(), handle, math.MaxInt64+1); got == nil || got.Code != ErrorOverflow {
		t.Fatalf("set-size overflow error = %#v, want overflow", got)
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
	openCalls := 0
	filesystem := &testFilesystem{openFileFn: func(name string, flag int, perm fs.FileMode) (fsapi.File, error) {
		openCalls++
		if name != "item" || flag != os.O_RDWR || perm != 0 {
			t.Fatalf("OpenFile(%q, %d, %v), want retained writable item", name, flag, perm)
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
	resource, ok := resources.Get(stream)
	if !ok {
		t.Fatal("stream resource disappeared")
	}
	output, ok := resource.(*fileOutputStreamResource)
	if !ok {
		t.Fatalf("stream resource = %T, want file output stream", resource)
	}
	deadline := time.Now().Add(time.Second)
	for {
		ready, checkErr := output.CheckWrite()
		if checkErr != nil {
			t.Fatalf("stream check-write error = %v", checkErr)
		}
		if ready != 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stream write did not complete")
		}
		time.Sleep(time.Millisecond)
	}
	streams.ResourceDropOutputStream(context.Background(), stream)
	streams.ResourceDropOutputStream(context.Background(), stream)
	NewTypesHost(resources).ResourceDropDescriptor(context.Background(), descriptor)

	if len(file.seekOffsets) != 1 || file.seekOffsets[0] != 11 || file.seekWhences[0] != 0 {
		t.Fatalf("Seek calls = offsets:%v whences:%v, want offset 11 from start once", file.seekOffsets, file.seekWhences)
	}
	if len(file.writes) != 1 || string(file.writes[0]) != "abc" {
		t.Fatalf("Write calls = %#v, want abc once", file.writes)
	}
	closeDeadline := time.Now().Add(time.Second)
	for file.closeCalls.Load() == 0 && time.Now().Before(closeDeadline) {
		time.Sleep(time.Millisecond)
	}
	if file.closeCalls.Load() != 1 {
		t.Fatalf("Close calls = %d, want 1", file.closeCalls.Load())
	}
	if openCalls != 1 {
		t.Fatalf("OpenFile calls = %d, want descriptor opened once and stream to retain it", openCalls)
	}
}

func TestR10DirectoryStreamTypedExhaustion(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" && runtime.GOOS != "darwin" && runtime.GOOS != "freebsd" {
		t.Skip("descriptor-root directory streams require a native descriptor opener")
	}
	host, _, descriptor, root := newDirectoryDescriptorHost(t)
	if err := os.WriteFile(root+"/file.txt", []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root+"/folder", 0700); err != nil {
		t.Fatal(err)
	}
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

func newDirectoryDescriptorHost(t *testing.T) (*TypesHost, *preview2.ResourceTable, uint32, string) {
	t.Helper()
	root := t.TempDir()
	filesystem, err := directoryfs.NewFS(root, 0700, false)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	t.Cleanup(func() { _ = filesystem.Close() })
	resources := preview2.NewResourceTable()
	t.Cleanup(func() { _ = resources.Close() })
	descriptor, err := newPreopenDescriptorResource(filesystem, ".", false)
	if err != nil {
		t.Fatalf("new preopen descriptor: %v", err)
	}
	return NewTypesHost(resources), resources, resources.Add(descriptor), root
}

func TestDescriptorOpenAtRetainsOpenedFileAcrossReplacement(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" && runtime.GOOS != "darwin" && runtime.GOOS != "freebsd" {
		t.Skip("descriptor-root open-at requires a native descriptor opener")
	}
	host, resources, rootDescriptor, root := newDirectoryDescriptorHost(t)
	path := root + "/item.txt"
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	opened, openErr := host.MethodDescriptorOpenAt(context.Background(), rootDescriptor, 0, "item.txt", 0, 1)
	if openErr != nil {
		t.Fatalf("open-at: %v", openErr)
	}
	replacement := root + "/replacement.txt"
	if err := os.WriteFile(replacement, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	got, readErr := host.MethodDescriptorRead(context.Background(), opened, 32, 0)
	if readErr != nil {
		t.Fatalf("descriptor read: %v", readErr)
	}
	data, ok := got[0].([]byte)
	if !ok || string(data) != "before" {
		t.Fatalf("retained descriptor read = %q, want before", data)
	}
	resources.Remove(opened)
}

func TestDescriptorOpenAtEnforcesAtomicOpenFlags(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" && runtime.GOOS != "darwin" && runtime.GOOS != "freebsd" {
		t.Skip("descriptor-root open-at requires a native descriptor opener")
	}
	host, _, rootDescriptor, root := newDirectoryDescriptorHost(t)
	if err := os.WriteFile(root+"/existing", []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, gotErr := host.MethodDescriptorOpenAt(context.Background(), rootDescriptor, 0, "existing", 1|4, 2); gotErr == nil || gotErr.Code != ErrorExist {
		t.Fatalf("create-exclusive existing error = %#v, want exist", gotErr)
	}
	truncated, gotErr := host.MethodDescriptorOpenAt(context.Background(), rootDescriptor, 0, "existing", 8, 2)
	if gotErr != nil {
		t.Fatalf("truncate open-at error = %v", gotErr)
	}
	stat, statErr := host.MethodDescriptorStat(context.Background(), truncated)
	if statErr != nil || stat.Size != 0 {
		t.Fatalf("truncated descriptor stat = %#v, %v; want zero size", stat, statErr)
	}
	if _, gotErr = host.MethodDescriptorOpenAt(context.Background(), rootDescriptor, 0, "existing", 2, 1); gotErr == nil || gotErr.Code != ErrorNotDirectory {
		t.Fatalf("directory regular-file error = %#v, want not-directory", gotErr)
	}
}

func TestDescriptorOpenAtZeroRightsCannotReadOrList(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" && runtime.GOOS != "darwin" && runtime.GOOS != "freebsd" {
		t.Skip("descriptor-root open-at requires a native descriptor opener")
	}
	host, _, rootDescriptor, root := newDirectoryDescriptorHost(t)
	if err := os.WriteFile(root+"/item", []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	file, openErr := host.MethodDescriptorOpenAt(context.Background(), rootDescriptor, 0, "item", 0, 0)
	if openErr != nil {
		t.Fatalf("open zero-right file descriptor: %v", openErr)
	}
	if _, readErr := host.MethodDescriptorRead(context.Background(), file, 1, 0); readErr == nil || readErr.Code != ErrorNotPermitted {
		t.Fatalf("zero-right descriptor read error = %#v, want not-permitted", readErr)
	}
	if _, streamErr := host.MethodDescriptorReadViaStream(context.Background(), file, 0); streamErr == nil || streamErr.Code != ErrorNotPermitted {
		t.Fatalf("zero-right descriptor read-via-stream error = %#v, want not-permitted", streamErr)
	}
	dir, openErr := host.MethodDescriptorOpenAt(context.Background(), rootDescriptor, 0, ".", 0, 0)
	if openErr != nil {
		t.Fatalf("open zero-right directory descriptor: %v", openErr)
	}
	if _, listErr := host.MethodDescriptorReadDirectory(context.Background(), dir); listErr == nil || listErr.Code != ErrorNotPermitted {
		t.Fatalf("zero-right directory list error = %#v, want not-permitted", listErr)
	}
}

func TestDescriptorRegularFileCannotTurnMutateDirectoryIntoWrite(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" && runtime.GOOS != "darwin" && runtime.GOOS != "freebsd" {
		t.Skip("descriptor-root open-at requires a native descriptor opener")
	}
	host, _, rootDescriptor, root := newDirectoryDescriptorHost(t)
	if err := os.WriteFile(root+"/item", []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	handle, openErr := host.MethodDescriptorOpenAt(context.Background(), rootDescriptor, 0, "item", 0, 32)
	if openErr != nil {
		t.Fatalf("open mutate-directory regular file: %v", openErr)
	}
	if _, writeErr := host.MethodDescriptorWriteViaStream(context.Background(), handle, 0); writeErr == nil || writeErr.Code != ErrorReadOnly {
		t.Fatalf("mutate-directory regular file write-via-stream error = %#v, want read-only", writeErr)
	}
	if _, appendErr := host.MethodDescriptorAppendViaStream(context.Background(), handle); appendErr == nil || appendErr.Code != ErrorReadOnly {
		t.Fatalf("mutate-directory regular file append-via-stream error = %#v, want read-only", appendErr)
	}
}

func TestDescriptorOpenAtNoFollowAndSafeFollow(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descriptor-root open-at requires Linux openat2")
	}
	host, _, rootDescriptor, root := newDirectoryDescriptorHost(t)
	if err := os.WriteFile(root+"/target", []byte("inside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", root+"/link"); err != nil {
		t.Fatal(err)
	}
	if _, gotErr := host.MethodDescriptorOpenAt(context.Background(), rootDescriptor, 0, "link", 0, 1); gotErr == nil || gotErr.Code != ErrorLoop {
		t.Fatalf("nofollow symlink error = %#v, want loop", gotErr)
	}
	opened, gotErr := host.MethodDescriptorOpenAt(context.Background(), rootDescriptor, 1, "link", 0, 1)
	if gotErr != nil {
		t.Fatalf("safe symlink follow open error = %v", gotErr)
	}
	got, readErr := host.MethodDescriptorRead(context.Background(), opened, 16, 0)
	if readErr != nil || string(got[0].([]byte)) != "inside" {
		t.Fatalf("safe symlink-follow read = %#v, %v", got, readErr)
	}
}

func TestDescriptorStatAtUsesRetainedDirectoryAfterRename(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" && runtime.GOOS != "darwin" && runtime.GOOS != "freebsd" {
		t.Skip("descriptor-root open-at requires a native descriptor opener")
	}
	host, _, rootDescriptor, root := newDirectoryDescriptorHost(t)
	if err := os.Mkdir(root+"/child", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/child/item", []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	child, openErr := host.MethodDescriptorOpenAt(context.Background(), rootDescriptor, 0, "child", 0, 1)
	if openErr != nil {
		t.Fatalf("open child directory: %v", openErr)
	}
	if err := os.Rename(root+"/child", root+"/moved"); err != nil {
		t.Fatal(err)
	}
	stat, statErr := host.MethodDescriptorStatAt(context.Background(), child, 0, "item")
	if statErr != nil || stat.Size != uint64(len("retained")) {
		t.Fatalf("stat-at through renamed descriptor = %#v, %v", stat, statErr)
	}
}

func TestDescriptorMutationsUseRetainedDirectoryCapability(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" && runtime.GOOS != "darwin" && runtime.GOOS != "freebsd" {
		t.Skip("descriptor-root mutation requires a native descriptor primitive")
	}
	host, _, rootDescriptor, root := newDirectoryDescriptorHost(t)
	flags, flagsErr := host.MethodDescriptorGetFlags(context.Background(), rootDescriptor)
	if flagsErr != nil || flags != 1|32 {
		t.Fatalf("preopen flags = %d, %v; want read|mutate-directory", flags, flagsErr)
	}
	if createErr := host.MethodDescriptorCreateDirectoryAt(context.Background(), rootDescriptor, "created"); createErr != nil {
		t.Fatalf("create-directory-at: %v", createErr)
	}
	if info, err := os.Stat(root + "/created"); err != nil || !info.IsDir() {
		t.Fatalf("created directory stat = %v, %v", info, err)
	}
	if err := os.WriteFile(root+"/old", []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if renameErr := host.MethodDescriptorRenameAt(context.Background(), rootDescriptor, "old", rootDescriptor, "new"); renameErr != nil {
		t.Fatalf("rename-at: %v", renameErr)
	}
	if _, err := os.Stat(root + "/new"); err != nil {
		t.Fatalf("renamed target missing: %v", err)
	}
	if unlinkErr := host.MethodDescriptorUnlinkFileAt(context.Background(), rootDescriptor, "new"); unlinkErr != nil {
		t.Fatalf("unlink-file-at: %v", unlinkErr)
	}
	if _, err := os.Stat(root + "/new"); !os.IsNotExist(err) {
		t.Fatalf("unlinked target stat err = %v, want not-exist", err)
	}
	if removeErr := host.MethodDescriptorRemoveDirectoryAt(context.Background(), rootDescriptor, "created"); removeErr != nil {
		t.Fatalf("remove-directory-at: %v", removeErr)
	}
	if createErr := host.MethodDescriptorCreateDirectoryAt(context.Background(), rootDescriptor, "nested"); createErr != nil {
		t.Fatalf("create nested directory: %v", createErr)
	}
	nested, nestedErr := host.MethodDescriptorOpenAt(context.Background(), rootDescriptor, 0, "nested", 2, 1|32)
	if nestedErr != nil {
		t.Fatalf("open mutable nested directory: %v", nestedErr)
	}
	if createErr := host.MethodDescriptorCreateDirectoryAt(context.Background(), nested, "child"); createErr != nil {
		t.Fatalf("create under mutable nested directory: %v", createErr)
	}
	if runtime.GOOS == "linux" {
		if err := os.Mkdir(root+"/real", 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("real", root+"/alias"); err != nil {
			t.Fatal(err)
		}
		if createErr := host.MethodDescriptorCreateDirectoryAt(context.Background(), rootDescriptor, "./alias/via-link"); createErr != nil {
			t.Fatalf("create through in-grant parent symlink: %v", createErr)
		}
		if info, err := os.Stat(root + "/real/via-link"); err != nil || !info.IsDir() {
			t.Fatalf("in-grant parent symlink target = %v, %v", info, err)
		}
		outside := t.TempDir()
		if err := os.Symlink(outside, root+"/escape"); err != nil {
			t.Fatal(err)
		}
		if createErr := host.MethodDescriptorCreateDirectoryAt(context.Background(), rootDescriptor, "escape/nope"); createErr == nil {
			t.Fatal("create through escaping parent symlink succeeded")
		}
		if _, err := os.Stat(outside + "/nope"); !os.IsNotExist(err) {
			t.Fatalf("escaping mutation created outside entry: %v", err)
		}
	}
}

func TestDirectoryStreamsHaveIndependentRetainedCursors(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" && runtime.GOOS != "darwin" && runtime.GOOS != "freebsd" {
		t.Skip("descriptor-root directory streams require a native descriptor opener")
	}
	host, _, rootDescriptor, root := newDirectoryDescriptorHost(t)
	for _, name := range []string{"one", "two", "three"} {
		if err := os.WriteFile(root+"/"+name, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	first, firstErr := host.MethodDescriptorReadDirectory(context.Background(), rootDescriptor)
	second, secondErr := host.MethodDescriptorReadDirectory(context.Background(), rootDescriptor)
	if firstErr != nil || secondErr != nil {
		t.Fatalf("read-directory streams: %v, %v", firstErr, secondErr)
	}
	readNames := func(stream uint32) map[string]bool {
		t.Helper()
		names := make(map[string]bool)
		for {
			entry, err := host.MethodDirectoryEntryStreamReadDirectoryEntry(context.Background(), stream)
			if err != nil {
				t.Fatalf("read-directory-entry: %v", err)
			}
			if entry == nil {
				return names
			}
			names[entry.Name] = true
		}
	}
	for label, names := range map[string]map[string]bool{"first": readNames(first), "second": readNames(second)} {
		for _, want := range []string{"one", "two", "three"} {
			if !names[want] {
				t.Fatalf("%s stream missed %q: %#v", label, want, names)
			}
		}
	}
}
