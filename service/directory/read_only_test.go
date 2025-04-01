package directory

import (
	"errors"
	"io"
	"testing"
	"testing/fstest"
	"time"
)

// TestReadOnlyFS_Open tests the Open method of ReadOnlyFS.
func TestReadOnlyFS_Open(t *testing.T) {
	// Create a simple in-memory filesystem for testing
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
			defer file.Close()

			// Verify file content
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

// TestReadOnlyFS_Stat tests the Stat method of ReadOnlyFS.
func TestReadOnlyFS_Stat(t *testing.T) {
	// Create a simple in-memory filesystem for testing
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

// TestReadOnlyFS_ReadDir tests the ReadDir method of ReadOnlyFS.
func TestReadOnlyFS_ReadDir(t *testing.T) {
	// Create a simple in-memory filesystem for testing
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
			wantLen: 3, // file1.txt, file2.txt, dir
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

// TestReadOnlyFS_OpenFile tests the OpenFile method of ReadOnlyFS.
func TestReadOnlyFS_OpenFile(t *testing.T) {
	// Create a simple in-memory filesystem for testing
	testFS := fstest.MapFS{
		"file.txt": {
			Data: []byte("hello world"),
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
			name:    "non-existent file",
			path:    "nonexistent.txt",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := readOnlyFS.OpenFile(tt.path, 0, 0)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadOnlyFS.OpenFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			defer file.Close()

			// Test write operation - should always fail
			_, err = file.Write([]byte("test"))
			if err == nil {
				t.Error("ReadOnlyFS.OpenFile() allowed Write operation on read-only file")
			}

			// Test sync operation - should always fail
			err = file.Sync()
			if err == nil {
				t.Error("ReadOnlyFS.OpenFile() allowed Sync operation on read-only file")
			}

			// Test seek operation - should always fail
			_, err = file.Seek(0, 0)
			if err == nil {
				t.Error("ReadOnlyFS.OpenFile() allowed Seek operation on read-only file")
			}

			// Verify file content is readable
			content := make([]byte, 100)
			n, err := file.Read(content)
			if err != nil && !errors.Is(err, io.EOF) {
				t.Errorf("Failed to read from file: %v", err)
			}
			expectedContent := testFS[tt.path].Data
			if string(content[:n]) != string(expectedContent) {
				t.Errorf("Content mismatch, got %q, want %q", content[:n], expectedContent)
			}
		})
	}
}

// TestReadOnlyFS_UnsupportedOperations tests the operations that are not supported.
func TestReadOnlyFS_UnsupportedOperations(t *testing.T) {
	testFS := fstest.MapFS{}
	readOnlyFS := NewReadOnlyFS(testFS)

	// Test Remove - should always fail
	err := readOnlyFS.Remove("any-path")
	if err == nil {
		t.Error("ReadOnlyFS.Remove() didn't return an error")
	}

	// Test Mkdir - should always fail
	err = readOnlyFS.Mkdir("any-path", 0755)
	if err == nil {
		t.Error("ReadOnlyFS.Mkdir() didn't return an error")
	}
}
