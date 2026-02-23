// SPDX-License-Identifier: MPL-2.0

package fs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"
	"time"
)

func TestReadOnlyFS_Open(t *testing.T) {
	testFS := fstest.MapFS{
		"file.txt": {
			Data: []byte("hello world"),
			Mode: 0644,
		},
		"dir/nested.txt": {
			Data: []byte("nested file"),
			Mode: 0644,
		},
	}

	readOnlyFS := NewReadOnlyFS(testFS)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "existing file",
			path:    "file.txt",
			wantErr: false,
		},
		{
			name:    "nested file",
			path:    "dir/nested.txt",
			wantErr: false,
		},
		{
			name:    "non-existent file",
			path:    "nonexistent.txt",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := readOnlyFS.Open(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadOnlyFS.Open() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			defer func() { _ = file.Close() }()

			if !tt.wantErr {
				content, err := io.ReadAll(file)
				if err != nil {
					t.Errorf("Failed to read file content: %v", err)
					return
				}

				expectedContent := testFS[tt.path].Data
				if string(content) != string(expectedContent) {
					t.Errorf("Content mismatch, got %q, want %q", content, expectedContent)
				}
			}
		})
	}
}

func TestReadOnlyFS_Stat(t *testing.T) {
	testFS := fstest.MapFS{
		"file.txt": {
			Data:    []byte("hello world"),
			Mode:    0644,
			ModTime: time.Now(),
		},
	}

	readOnlyFS := NewReadOnlyFS(testFS)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "existing file",
			path:    "file.txt",
			wantErr: false,
		},
		{
			name:    "non-existent file",
			path:    "nonexistent.txt",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := readOnlyFS.Stat(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadOnlyFS.Stat() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if info.Name() != tt.path {
					t.Errorf("File name mismatch, got %q, want %q", info.Name(), tt.path)
				}
				if info.Size() != int64(len(testFS[tt.path].Data)) {
					t.Errorf("File size mismatch, got %d, want %d", info.Size(), len(testFS[tt.path].Data))
				}
			}
		})
	}
}

func TestReadOnlyFS_ReadDir(t *testing.T) {
	testFS := fstest.MapFS{
		"file1.txt": {
			Data: []byte("file1"),
			Mode: 0644,
		},
		"file2.txt": {
			Data: []byte("file2"),
			Mode: 0644,
		},
		"dir/nested.txt": {
			Data: []byte("nested file"),
			Mode: 0644,
		},
	}

	readOnlyFS := NewReadOnlyFS(testFS)

	tests := []struct {
		name    string
		path    string
		wantLen int
		wantErr bool
	}{
		{
			name:    "root directory",
			path:    ".",
			wantLen: 3,
			wantErr: false,
		},
		{
			name:    "non-existent directory",
			path:    "nonexistent",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := readOnlyFS.ReadDir(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadOnlyFS.ReadDir() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(entries) != tt.wantLen {
				t.Errorf("ReadOnlyFS.ReadDir() returned %d entries, want %d", len(entries), tt.wantLen)
			}
		})
	}
}

func TestReadOnlyFS_OpenFile(t *testing.T) {
	testFS := fstest.MapFS{
		"file.txt": {
			Data: []byte("hello world"),
			Mode: 0644,
		},
	}

	readOnlyFS := NewReadOnlyFS(testFS)

	t.Run("read only mode succeeds", func(t *testing.T) {
		file, err := readOnlyFS.OpenFile("file.txt", os.O_RDONLY, 0)
		if err != nil {
			t.Fatalf("OpenFile() error = %v", err)
		}
		defer func() { _ = file.Close() }()

		content := make([]byte, 100)
		n, err := file.Read(content)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Errorf("Failed to read from file: %v", err)
		}
		if string(content[:n]) != "hello world" {
			t.Errorf("Content mismatch, got %q", content[:n])
		}
	})

	t.Run("write mode fails", func(t *testing.T) {
		_, err := readOnlyFS.OpenFile("file.txt", os.O_WRONLY, 0)
		if err == nil {
			t.Error("OpenFile() with O_WRONLY should fail")
		}
		if !errors.Is(err, fs.ErrPermission) {
			t.Errorf("Expected fs.ErrPermission, got %v", err)
		}
	})

	t.Run("rdwr mode fails", func(t *testing.T) {
		_, err := readOnlyFS.OpenFile("file.txt", os.O_RDWR, 0)
		if err == nil {
			t.Error("OpenFile() with O_RDWR should fail")
		}
	})

	t.Run("create mode fails", func(t *testing.T) {
		_, err := readOnlyFS.OpenFile("new.txt", os.O_CREATE, 0)
		if err == nil {
			t.Error("OpenFile() with O_CREATE should fail")
		}
	})

	t.Run("non-existent file fails", func(t *testing.T) {
		_, err := readOnlyFS.OpenFile("nonexistent.txt", os.O_RDONLY, 0)
		if err == nil {
			t.Error("OpenFile() for non-existent file should fail")
		}
	})
}

func TestReadOnlyFS_FileOperations(t *testing.T) {
	testFS := fstest.MapFS{
		"file.txt": {
			Data: []byte("hello world"),
			Mode: 0644,
		},
	}

	readOnlyFS := NewReadOnlyFS(testFS)
	file, err := readOnlyFS.OpenFile("file.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	defer func() { _ = file.Close() }()

	t.Run("Write fails", func(t *testing.T) {
		_, err := file.Write([]byte("test"))
		if err == nil {
			t.Error("Write() should fail on read-only file")
		}
		if !errors.Is(err, fs.ErrPermission) {
			t.Errorf("Expected fs.ErrPermission, got %v", err)
		}
	})

	t.Run("Seek fails", func(t *testing.T) {
		_, err := file.Seek(0, 0)
		if err == nil {
			t.Error("Seek() should fail on read-only file")
		}
		if !errors.Is(err, fs.ErrPermission) {
			t.Errorf("Expected fs.ErrPermission, got %v", err)
		}
	})

	t.Run("Sync succeeds", func(t *testing.T) {
		err := file.Sync()
		if err != nil {
			t.Errorf("Sync() should succeed (no-op), got %v", err)
		}
	})
}

func TestReadOnlyFS_UnsupportedOperations(t *testing.T) {
	testFS := fstest.MapFS{}
	readOnlyFS := NewReadOnlyFS(testFS)

	t.Run("Remove fails", func(t *testing.T) {
		err := readOnlyFS.Remove("any-path")
		if err == nil {
			t.Error("Remove() should fail")
		}
		if !errors.Is(err, fs.ErrPermission) {
			t.Errorf("Expected fs.ErrPermission, got %v", err)
		}
	})

	t.Run("Mkdir fails", func(t *testing.T) {
		err := readOnlyFS.Mkdir("any-path", 0755)
		if err == nil {
			t.Error("Mkdir() should fail")
		}
		if !errors.Is(err, fs.ErrPermission) {
			t.Errorf("Expected fs.ErrPermission, got %v", err)
		}
	})
}
