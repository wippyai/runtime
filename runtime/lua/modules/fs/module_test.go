package fs

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"

	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/logs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap/zaptest"
)

// mockResource implements the resource.Resource interface
type mockResource struct {
	resValue any
	released bool
}

func (m *mockResource) Get() (any, error) {
	return m.resValue, nil
}

func (m *mockResource) Release() {
	m.released = true
}

func (m *mockResource) Mode() resource.AccessMode {
	return resource.ModeNormal
}

// mockResourceRegistry is a simple mock for the resource registry
type mockResourceRegistry struct {
	resources map[registry.ID]resource.Resource[any]
}

func (m *mockResourceRegistry) Acquire(
	_ context.Context,
	id registry.ID,
	_ resource.AccessMode,
) (resource.Resource[any], error) {
	res, ok := m.resources[id]
	if !ok {
		return nil, resource.ErrResourceNotFound
	}
	return res, nil
}

func (m *mockResourceRegistry) List() ([]registry.ID, error) {
	ids := make([]registry.ID, 0, len(m.resources))
	for id := range m.resources {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *mockResourceRegistry) Exists(id registry.ID) bool {
	_, ok := m.resources[id]
	return ok
}

// mockFileInfo implements fs.FileInfo interface for testing
type mockFileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
	isDir   bool
}

func (m mockFileInfo) Name() string       { return m.name }
func (m mockFileInfo) Size() int64        { return m.size }
func (m mockFileInfo) Mode() fs.FileMode  { return m.mode }
func (m mockFileInfo) ModTime() time.Time { return m.modTime }
func (m mockFileInfo) IsDir() bool        { return m.isDir }
func (m mockFileInfo) Sys() any           { return nil }

// mockDirEntry implements fs.DirEntry interface for testing
type mockDirEntry struct {
	name  string
	isDir bool
	info  fs.FileInfo
}

func (m mockDirEntry) Name() string { return m.name }
func (m mockDirEntry) IsDir() bool  { return m.isDir }
func (m mockDirEntry) Type() fs.FileMode {
	if m.info != nil {
		return m.info.Mode()
	}
	if m.isDir {
		return fs.ModeDir
	}
	return 0
}

func (m mockDirEntry) Info() (fs.FileInfo, error) {
	if m.info != nil {
		return m.info, nil
	}
	return mockFileInfo{name: m.name, isDir: m.isDir}, nil
}

// mockFile implements the fsapi.File interface for testing
type mockFile struct {
	name     string
	content  *strings.Reader
	writeBuf *strings.Builder
	closed   bool
	mutex    sync.Mutex
}

func newMockFile(name, content string) *mockFile {
	return &mockFile{
		name:     name,
		content:  strings.NewReader(content),
		writeBuf: &strings.Builder{},
	}
}

func (m *mockFile) Read(p []byte) (int, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.closed {
		return 0, fs.ErrClosed
	}
	return m.content.Read(p)
}

func (m *mockFile) Write(p []byte) (int, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.closed {
		return 0, fs.ErrClosed
	}
	return m.writeBuf.Write(p)
}

func (m *mockFile) Close() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.closed {
		// Don't return an error for already closed files
		return nil
	}
	m.closed = true
	return nil
}

func (m *mockFile) Stat() (fs.FileInfo, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.closed {
		return nil, fs.ErrClosed
	}
	return mockFileInfo{
		name:    m.name,
		size:    int64(m.content.Len()),
		mode:    0644,
		modTime: time.Now(),
		isDir:   false,
	}, nil
}

func (m *mockFile) Seek(offset int64, whence int) (int64, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.closed {
		return 0, fs.ErrClosed
	}
	return m.content.Seek(offset, whence)
}

func (m *mockFile) Sync() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.closed {
		return fs.ErrClosed
	}
	return nil
}

// mockFS implements the fsapi.FS interface for testing
type mockFS struct {
	files    map[string]*mockFile
	dirs     map[string]bool
	mutex    sync.RWMutex
	openErrs map[string]error // Simulate errors for specific paths
}

func newMockFS() *mockFS {
	return &mockFS{
		files:    make(map[string]*mockFile),
		dirs:     make(map[string]bool),
		openErrs: make(map[string]error),
	}
}

func (m *mockFS) Open(name string) (fs.File, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// Check for simulated errors
	if err, ok := m.openErrs[name]; ok {
		return nil, err
	}

	// Check if it's a directory
	if m.dirs[name] {
		// Return a directory error for simplicity in this example
		return nil, fs.ErrInvalid
	}

	// Check if it's a file
	file, exists := m.files[name]
	if !exists {
		return nil, fs.ErrNotExist
	}

	// Create a new buffer with the same content to avoid concurrent access issues
	buf := make([]byte, file.content.Size())
	_, err := file.content.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	// Reset the position
	_, err = file.content.Seek(0, 0)
	if err != nil {
		return nil, err
	}

	return newMockFile(file.name, string(buf)), nil
}

func (m *mockFS) OpenFile(name string, flag int, _ fs.FileMode) (fsapi.File, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Check for simulated errors
	if err, ok := m.openErrs[name]; ok {
		return nil, err
	}

	var file *mockFile
	var exists bool

	// Check if file exists
	file, exists = m.files[name]

	// Handle different flags
	if flag&os.O_CREATE != 0 {
		if exists && flag&os.O_EXCL != 0 {
			return nil, fs.ErrExist
		}

		if !exists {
			// Create new file
			file = newMockFile(name, "")
			m.files[name] = file
		} else if flag&os.O_TRUNC != 0 {
			// Truncate existing file
			file = newMockFile(name, "")
			m.files[name] = file
		}
	} else if !exists {
		return nil, fs.ErrNotExist
	}

	return file, nil
}

func (m *mockFS) Stat(name string) (fs.FileInfo, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// Check for simulated errors
	if err, ok := m.openErrs[name]; ok {
		return nil, err
	}

	// Check if it's a directory
	if m.dirs[name] {
		return mockFileInfo{
			name:    name,
			size:    0,
			mode:    fs.ModeDir | 0755,
			modTime: time.Now(),
			isDir:   true,
		}, nil
	}

	// Check if it's a file
	file, exists := m.files[name]
	if !exists {
		return nil, fs.ErrNotExist
	}

	return file.Stat()
}

func (m *mockFS) ReadDir(name string) ([]fs.DirEntry, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// Check for simulated errors
	if err, ok := m.openErrs[name]; ok {
		return nil, err
	}

	// Check if directory exists
	if !m.dirs[name] && name != "." {
		return nil, fs.ErrNotExist
	}

	// Create entries for the directory
	entries := make([]fs.DirEntry, 0)

	// Add subdirectories
	prefix := name + "/"
	if name == "." {
		prefix = ""
	}

	// Add directories
	for dir := range m.dirs {
		if dir == name {
			continue // Skip self
		}

		// Check if the directory is a direct child of name
		if strings.HasPrefix(dir, prefix) {
			relName := strings.TrimPrefix(dir, prefix)
			if !strings.Contains(relName, "/") { // Only direct children
				entries = append(entries, mockDirEntry{
					name:  relName,
					isDir: true,
				})
			}
		}
	}

	// Add files
	for filename, file := range m.files {
		if strings.HasPrefix(filename, prefix) {
			relName := strings.TrimPrefix(filename, prefix)
			if !strings.Contains(relName, "/") { // Only direct children
				info, _ := file.Stat()
				entries = append(entries, mockDirEntry{
					name:  relName,
					isDir: false,
					info:  info,
				})
			}
		}
	}

	return entries, nil
}

func (m *mockFS) Remove(name string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Check for simulated errors
	if err, ok := m.openErrs[name]; ok {
		return err
	}

	// Check if it's a directory
	if m.dirs[name] {
		delete(m.dirs, name)
		return nil
	}

	// Check if it's a file
	if _, exists := m.files[name]; exists {
		delete(m.files, name)
		return nil
	}

	return fs.ErrNotExist
}

func (m *mockFS) Mkdir(name string, _ fs.FileMode) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Check for simulated errors
	if err, ok := m.openErrs[name]; ok {
		return err
	}

	// Check if already exists
	if m.dirs[name] {
		return fs.ErrExist
	}
	if _, exists := m.files[name]; exists {
		return fs.ErrExist
	}

	// Create the directory
	m.dirs[name] = true
	return nil
}

// AddFile adds a file to the mock filesystem
func (m *mockFS) AddFile(name, content string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.files[name] = newMockFile(name, content)
}

// AddDir adds a directory to the mock filesystem
func (m *mockFS) AddDir(name string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.dirs[name] = true
}

// AddError simulates errors for specific paths
func (m *mockFS) AddError(path string, err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.openErrs[path] = err
}

// mockFSRegistry implements the fsapi.Registry interface for testing
type mockFSRegistry struct {
	filesystems map[string]fsapi.FS
}

func (m *mockFSRegistry) GetFS(name string) (fsapi.FS, bool) {
	fsi, ok := m.filesystems[name]
	return fsi, ok
}

// setupLuaWithFS sets up a Lua state with the FS module and a mock filesystem
func setupLuaWithFS(t *testing.T, mockRes *mockResource) (
	*engine.CoroutineVM,
	*engine.Runner,
	context.Context,
) {
	logger := zaptest.NewLogger(t)

	// Create the FS module
	module := NewFSModule()

	// Create a mock resource registry with our test filesystem
	mockRegistry := &mockResourceRegistry{
		resources: map[registry.ID]resource.Resource[any]{
			registry.ParseID("app:test_fs"): mockRes,
		},
	}

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Get the Lua state
	L := vm.State()

	// Register the FS module
	L.PreloadModule(module.Name(), module.Loader)

	// Create a runner with the coroutine layer
	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

	// Create context with frame
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	// Add the resource registry to the context
	ctx = resource.WithRegistry(ctx, mockRegistry)
	ctx = logs.WithLogger(ctx, logger)

	return vm, runner, ctx
}

// TestModuleLoading tests basic loading of the FS module
func TestModuleLoading(t *testing.T) {
	// Create the FS module
	module := NewFSModule()

	// Create a VM
	L := lua.NewState()
	defer L.Close()

	// Register the FS module
	L.PreloadModule(module.Name(), module.Loader)

	// Load the module and check basic properties
	err := L.DoString(`
		local fs = require("fs")
		assert(fs ~= nil, "fs module should not be nil")
		assert(fs.type ~= nil, "fs.type should not be nil")
		assert(fs.type.FILE == "file", "fs.type.FILE should be 'file'")
		assert(fs.type.DIR == "directory", "fs.type.DIR should be 'directory'")
		assert(fs.seek ~= nil, "fs.seek should not be nil")
		assert(fs.seek.SET == "set", "fs.seek.SET should be 'set'")
		assert(fs.seek.CUR == "cur", "fs.seek.CUR should be 'cur'")
		assert(fs.seek.END == "end", "fs.seek.END should be 'end'")
		assert(fs.get ~= nil, "fs.get should not be nil")
	`)
	assert.NoError(t, err, "Module should load without errors")
}

// TestFSGet tests the fs.get function
func TestFSGet(t *testing.T) {
	// Create a mock filesystem
	mockFS := newMockFS()
	mockFS.AddFile("test.txt", "Hello, World!")
	mockFS.AddDir("testdir")

	// Create our resource that will be tracked for release
	mockRes := &mockResource{
		resValue: mockFS,
	}

	// Setup Lua with the test filesystem
	vm, runner, ctx := setupLuaWithFS(t, mockRes)
	defer vm.Close()

	// Create a mock registry with the filesystem
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": mockFS,
		},
	}

	// Inject our filesystem registry into the context
	ctx = fsapi.WithRegistry(ctx, fsRegistry)

	// Imports our test function into the VM
	err := vm.Import(`
		function test_fs_get()
			local fs = require("fs")
			-- Get the filesystem
			local fsObj, err = fs.get("test_fs")
			if err then 
				error("Error getting FS: " .. err)
			end
			
			-- Try some basic operations
			local exists, err = fsObj:exists("test.txt")
			if err then
				error("Error checking file existence: " .. err)
			end
			
			local isDir, err = fsObj:isdir("testdir")
			if err then
				error("Error checking if dir: " .. err)
			end
			
			-- Return results for verification
			return {
				file_exists = exists,
				is_directory = isDir
			}
		end
	`, "test", "test_fs_get")
	require.NoError(t, err, "Failed to import test function")

	// Serve the function using the runner
	result, err := runner.Execute(ctx, "test_fs_get")
	require.NoError(t, err, "Lua execution failed")

	// Verify the results
	resultTable := result.(*lua.LTable)
	fileExists := resultTable.RawGetString("file_exists").(lua.LBool)
	isDirectory := resultTable.RawGetString("is_directory").(lua.LBool)

	assert.True(t, bool(fileExists), "File should exist")
	assert.True(t, bool(isDirectory), "Directory should exist")
}
