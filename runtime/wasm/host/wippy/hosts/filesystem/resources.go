// SPDX-License-Identifier: MPL-2.0

package filesystem

import (
	"io"

	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

// descriptorResource represents an open file or directory descriptor backed by fsapi.FS.
type descriptorResource struct {
	fs       fsapi.FS
	path     string
	isDir    bool
	readOnly bool
	position int64
}

func newDescriptorResource(fs fsapi.FS, path string, isDir bool, readOnly bool) *descriptorResource {
	return &descriptorResource{
		fs:       fs,
		path:     path,
		isDir:    isDir,
		readOnly: readOnly,
	}
}

func (d *descriptorResource) Type() preview2.ResourceType { return preview2.ResourceDescriptor }
func (d *descriptorResource) Drop()                       {}

// fileOutputStreamResource writes to a file via fsapi.File.
type fileOutputStreamResource struct {
	file   fsapi.File
	closed bool
}

func newFileOutputStreamResource(file fsapi.File) *fileOutputStreamResource {
	return &fileOutputStreamResource{file: file}
}

func (s *fileOutputStreamResource) Type() preview2.ResourceType { return preview2.ResourceOutputStream }

func (s *fileOutputStreamResource) Drop() {
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
	s.closed = true
}

func (s *fileOutputStreamResource) Write(data []byte) error {
	if s.closed || s.file == nil {
		return &preview2.StreamError{Closed: true}
	}
	_, err := s.file.Write(data)
	if err != nil {
		return &preview2.StreamError{LastOpFailed: true}
	}
	return nil
}

func (s *fileOutputStreamResource) CheckWrite() (uint64, error) {
	if s.closed || s.file == nil {
		return 0, &preview2.StreamError{Closed: true}
	}
	return preview2.DefaultBufferSize, nil
}

func (s *fileOutputStreamResource) Flush() error {
	if s.closed || s.file == nil {
		return &preview2.StreamError{Closed: true}
	}
	return s.file.Sync()
}

// readAllFrom opens a file via the FS, reads all content, and closes it.
func readAllFrom(fs fsapi.FS, path string) ([]byte, error) {
	f, err := fs.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}
