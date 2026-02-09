package fileserve

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	fsapi "github.com/wippyai/runtime/api/fs"
)

// MockFS implements fsapi.FS for testing
type MockFS struct {
	files map[string][]byte
}

func NewMockFS() *MockFS {
	return &MockFS{
		files: make(map[string][]byte),
	}
}

func (m *MockFS) AddFile(path string, content []byte) {
	m.files[path] = content
}

func (m *MockFS) Open(name string) (fs.File, error) {
	content, ok := m.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return &MockFile{
		name:    filepath.Base(name),
		content: content,
	}, nil
}

func (m *MockFS) Stat(name string) (fs.FileInfo, error) {
	content, ok := m.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return &MockFileInfo{
		name: filepath.Base(name),
		size: int64(len(content)),
	}, nil
}

func (m *MockFS) ReadDir(_ string) ([]fs.DirEntry, error) {
	return nil, nil
}

func (m *MockFS) OpenFile(name string, _ int, _ fs.FileMode) (fsapi.File, error) {
	content, ok := m.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return &MockFile{
		name:    filepath.Base(name),
		content: content,
	}, nil
}

func (m *MockFS) Remove(name string) error {
	delete(m.files, name)
	return nil
}

func (m *MockFS) Mkdir(_ string, _ fs.FileMode) error {
	return nil
}

func (m *MockFS) Lstat(name string) (fs.FileInfo, error) {
	return m.Stat(name)
}

func (m *MockFS) Rename(_, _ string) error {
	return nil
}

func (m *MockFS) Truncate(_ string, _ int64) error {
	return nil
}

func (m *MockFS) Chtimes(_ string, _, _ time.Time) error {
	return nil
}

// MockFile implements fsapi.File
type MockFile struct {
	name    string
	content []byte
	offset  int64
}

func (f *MockFile) Read(p []byte) (n int, err error) {
	if f.offset >= int64(len(f.content)) {
		return 0, os.ErrClosed
	}
	n = copy(p, f.content[f.offset:])
	f.offset += int64(n)
	return n, nil
}

func (f *MockFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case 0:
		f.offset = offset
	case 1:
		f.offset += offset
	case 2:
		f.offset = int64(len(f.content)) + offset
	}
	return f.offset, nil
}

func (f *MockFile) Close() error {
	return nil
}

func (f *MockFile) Stat() (fs.FileInfo, error) {
	return &MockFileInfo{
		name: f.name,
		size: int64(len(f.content)),
	}, nil
}

func (f *MockFile) Write(p []byte) (n int, err error) {
	f.content = append(f.content, p...)
	return len(p), nil
}

func (f *MockFile) Sync() error {
	return nil
}

type MockFileInfo struct {
	name string
	size int64
}

func (fi *MockFileInfo) Name() string       { return fi.name }
func (fi *MockFileInfo) Size() int64        { return fi.size }
func (fi *MockFileInfo) Mode() fs.FileMode  { return 0644 }
func (fi *MockFileInfo) ModTime() time.Time { return time.Now() }
func (fi *MockFileInfo) IsDir() bool        { return false }
func (fi *MockFileInfo) Sys() any           { return nil }

// MockFSRegistry implements FSRegistry
type MockFSRegistry struct {
	filesystems map[string]fsapi.FS
}

func NewMockFSRegistry() *MockFSRegistry {
	return &MockFSRegistry{
		filesystems: make(map[string]fsapi.FS),
	}
}

func (r *MockFSRegistry) Register(id string, fs fsapi.FS) {
	r.filesystems[id] = fs
}

func (r *MockFSRegistry) GetFS(id string) (fsapi.FS, bool) {
	f, ok := r.filesystems[id]
	return f, ok
}

func TestCreateFileServeMiddleware(t *testing.T) {
	t.Run("serve file with X-Sendfile header", func(t *testing.T) {
		mockFS := NewMockFS()
		mockFS.AddFile("test.txt", []byte("hello world"))

		registry := NewMockFSRegistry()
		registry.Register("uploads", mockFS)

		middleware := CreateFileServeMiddleware(map[string]string{
			OptionFS: "uploads",
		}, registry)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(XSendfileHeader, "test.txt")
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "hello world", w.Body.String())
		assert.Equal(t, "", w.Header().Get(XSendfileHeader))
	})

	t.Run("serve file with X-File-Path header (legacy)", func(t *testing.T) {
		mockFS := NewMockFS()
		mockFS.AddFile("legacy.txt", []byte("legacy content"))

		registry := NewMockFSRegistry()
		registry.Register("files", mockFS)

		middleware := CreateFileServeMiddleware(map[string]string{
			OptionFS: "files",
		}, registry)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(XFilePathHeader, "legacy.txt")
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "legacy content", w.Body.String())
		assert.Equal(t, "", w.Header().Get(XFilePathHeader))
	})

	t.Run("set download filename with X-File-Name header", func(t *testing.T) {
		mockFS := NewMockFS()
		mockFS.AddFile("document.pdf", []byte("PDF content"))

		registry := NewMockFSRegistry()
		registry.Register("docs", mockFS)

		middleware := CreateFileServeMiddleware(map[string]string{
			OptionFS: "docs",
		}, registry)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(XSendfileHeader, "document.pdf")
			w.Header().Set(XFileNameHeader, "MyReport.pdf")
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Disposition"), `attachment; filename="MyReport.pdf"`)
		assert.Equal(t, "", w.Header().Get(XFileNameHeader))
	})

	t.Run("no file header means passthrough", func(t *testing.T) {
		registry := NewMockFSRegistry()

		middleware := CreateFileServeMiddleware(map[string]string{
			OptionFS: "any",
		}, registry)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("normal response"))
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "normal response", w.Body.String())
	})

	t.Run("error when fs option not configured", func(t *testing.T) {
		registry := NewMockFSRegistry()

		middleware := CreateFileServeMiddleware(map[string]string{}, registry)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(XSendfileHeader, "test.txt")
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "fs option not configured")
	})

	t.Run("error when filesystem not found", func(t *testing.T) {
		registry := NewMockFSRegistry()

		middleware := CreateFileServeMiddleware(map[string]string{
			OptionFS: "nonexistent",
		}, registry)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(XSendfileHeader, "test.txt")
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "filesystem")
		assert.Contains(t, w.Body.String(), "not found")
	})

	t.Run("error when file not found", func(t *testing.T) {
		mockFS := NewMockFS()
		registry := NewMockFSRegistry()
		registry.Register("files", mockFS)

		middleware := CreateFileServeMiddleware(map[string]string{
			OptionFS: "files",
		}, registry)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(XSendfileHeader, "nonexistent.txt")
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "file not found")
	})

	t.Run("error on invalid file paths", func(t *testing.T) {
		mockFS := NewMockFS()
		registry := NewMockFSRegistry()
		registry.Register("files", mockFS)

		middleware := CreateFileServeMiddleware(map[string]string{
			OptionFS: "files",
		}, registry)

		invalidPaths := []string{".", ".."}

		for _, path := range invalidPaths {
			t.Run("path="+path, func(t *testing.T) {
				handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set(XSendfileHeader, path)
				}))

				req := httptest.NewRequest("GET", "http://example.com/test", nil)
				w := httptest.NewRecorder()

				handler.ServeHTTP(w, req)

				assert.Equal(t, http.StatusBadRequest, w.Code)
				assert.Contains(t, w.Body.String(), "invalid file path")
			})
		}
	})

	t.Run("X-Sendfile takes precedence over X-File-Path", func(t *testing.T) {
		mockFS := NewMockFS()
		mockFS.AddFile("sendfile.txt", []byte("sendfile wins"))
		mockFS.AddFile("filepath.txt", []byte("filepath loses"))

		registry := NewMockFSRegistry()
		registry.Register("files", mockFS)

		middleware := CreateFileServeMiddleware(map[string]string{
			OptionFS: "files",
		}, registry)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(XSendfileHeader, "sendfile.txt")
			w.Header().Set(XFilePathHeader, "filepath.txt")
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "sendfile wins", w.Body.String())
	})

	t.Run("legacy fs option key fallback", func(t *testing.T) {
		mockFS := NewMockFS()
		mockFS.AddFile("test.txt", []byte("legacy key works"))

		registry := NewMockFSRegistry()
		registry.Register("legacy", mockFS)

		middleware := CreateFileServeMiddleware(map[string]string{
			legacyFS: "legacy", // Old key format
		}, registry)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(XSendfileHeader, "test.txt")
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "legacy key works", w.Body.String())
	})

	t.Run("dot-separated key takes precedence over legacy", func(t *testing.T) {
		mockFS1 := NewMockFS()
		mockFS1.AddFile("file.txt", []byte("new key wins"))

		mockFS2 := NewMockFS()
		mockFS2.AddFile("file.txt", []byte("legacy loses"))

		registry := NewMockFSRegistry()
		registry.Register("new", mockFS1)
		registry.Register("legacy", mockFS2)

		middleware := CreateFileServeMiddleware(map[string]string{
			OptionFS: "new",    // New key
			legacyFS: "legacy", // Old key
		}, registry)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(XSendfileHeader, "file.txt")
		}))

		req := httptest.NewRequest("GET", "http://example.com/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "new key wins", w.Body.String())
	})
}
