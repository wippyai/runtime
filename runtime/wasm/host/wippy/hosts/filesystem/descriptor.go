// SPDX-License-Identifier: MPL-2.0

package filesystem

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"sync"

	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

// retainedFile owns one already-open host file. A descriptor and every stream
// derived from it share this owner; they never reopen a pathname. This is the
// identity boundary required by WASI descriptors: replacing a pathname after
// open must not change the object accessed through the descriptor.
//
// Most fs.File implementations do not provide ReaderAt/WriterAt. The fallback
// serializes seek/read or seek/write so offset operations remain correct when
// several guest streams share one descriptor.
type retainedFile struct {
	file   fs.File
	lifeMu sync.Mutex
	ioMu   sync.Mutex
	refs   uint32
	closed bool
}

func newRetainedFile(file fs.File) (*retainedFile, error) {
	if file == nil {
		return nil, errors.New("filesystem descriptor has no file handle")
	}
	return &retainedFile{file: file, refs: 1}, nil
}

func (f *retainedFile) retain() error {
	f.lifeMu.Lock()
	defer f.lifeMu.Unlock()
	if f.closed {
		return fs.ErrClosed
	}
	f.refs++
	return nil
}

// borrow keeps the owner alive while a capability operation consumes the raw
// directory handle. The caller must invoke the returned release exactly once.
func (f *retainedFile) borrow() (fs.File, func(), error) {
	if err := f.retain(); err != nil {
		return nil, nil, err
	}
	f.lifeMu.Lock()
	file := f.file
	f.lifeMu.Unlock()
	if file == nil {
		f.release()
		return nil, nil, fs.ErrClosed
	}
	return file, f.release, nil
}

func (f *retainedFile) release() {
	f.lifeMu.Lock()
	if f.refs == 0 {
		f.lifeMu.Unlock()
		return
	}
	f.refs--
	if f.refs != 0 || f.closed {
		f.lifeMu.Unlock()
		return
	}
	f.closed = true
	file := f.file
	f.file = nil
	f.lifeMu.Unlock()
	_ = file.Close()
}

func (f *retainedFile) stat() (fs.FileInfo, error) {
	if err := f.retain(); err != nil {
		return nil, err
	}
	defer f.release()
	f.lifeMu.Lock()
	file := f.file
	f.lifeMu.Unlock()
	if file == nil {
		return nil, fs.ErrClosed
	}
	return file.Stat()
}

func (f *retainedFile) readAt(p []byte, offset int64) (int, error) {
	if err := f.retain(); err != nil {
		return 0, err
	}
	defer f.release()
	f.lifeMu.Lock()
	file := f.file
	f.lifeMu.Unlock()
	if file == nil {
		return 0, fs.ErrClosed
	}
	f.ioMu.Lock()
	defer f.ioMu.Unlock()
	if reader, ok := file.(io.ReaderAt); ok {
		return reader.ReadAt(p, offset)
	}
	seeker, ok := file.(io.Seeker)
	if !ok {
		return 0, fs.ErrInvalid
	}
	if _, err := seeker.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	return file.Read(p)
}

func (f *retainedFile) writeAt(p []byte, offset int64) (int, error) {
	if err := f.retain(); err != nil {
		return 0, err
	}
	defer f.release()
	f.lifeMu.Lock()
	file := f.file
	f.lifeMu.Unlock()
	if file == nil {
		return 0, fs.ErrClosed
	}
	f.ioMu.Lock()
	defer f.ioMu.Unlock()
	writer, ok := file.(io.Writer)
	if !ok {
		return 0, fs.ErrPermission
	}
	if at, ok := file.(io.WriterAt); ok {
		return at.WriteAt(p, offset)
	}
	seeker, ok := file.(io.Seeker)
	if !ok {
		return 0, fs.ErrInvalid
	}
	if _, err := seeker.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	return writer.Write(p)
}

func (f *retainedFile) append(p []byte) (int, error) {
	if err := f.retain(); err != nil {
		return 0, err
	}
	defer f.release()
	f.lifeMu.Lock()
	file := f.file
	f.lifeMu.Unlock()
	if file == nil {
		return 0, fs.ErrClosed
	}
	appender, ok := file.(interface{ Append([]byte) (int, error) })
	if !ok {
		// WASI append streams require append-at-write semantics. Seek followed
		// by Write is only serialized within this retained owner; a separately
		// opened descriptor could still interleave between the two operations.
		// Providers must opt into a kernel-backed atomic append primitive.
		return 0, errors.ErrUnsupported
	}
	return appender.Append(p)
}

func (f *retainedFile) sync() error {
	if err := f.retain(); err != nil {
		return err
	}
	defer f.release()
	f.lifeMu.Lock()
	file := f.file
	f.lifeMu.Unlock()
	if file == nil {
		return fs.ErrClosed
	}
	f.ioMu.Lock()
	defer f.ioMu.Unlock()
	syncer, ok := file.(interface{ Sync() error })
	if !ok {
		return errors.ErrUnsupported
	}
	return syncer.Sync()
}

func (f *retainedFile) truncate(size int64) error {
	if err := f.retain(); err != nil {
		return err
	}
	defer f.release()
	f.lifeMu.Lock()
	file := f.file
	f.lifeMu.Unlock()
	if file == nil {
		return fs.ErrClosed
	}
	f.ioMu.Lock()
	defer f.ioMu.Unlock()
	truncater, ok := file.(interface{ Truncate(int64) error })
	if !ok {
		return errors.ErrUnsupported
	}
	return truncater.Truncate(size)
}

func (f *retainedFile) readDirOne() (fs.DirEntry, error) {
	if err := f.retain(); err != nil {
		return nil, err
	}
	defer f.release()
	f.lifeMu.Lock()
	file := f.file
	f.lifeMu.Unlock()
	if file == nil {
		return nil, fs.ErrClosed
	}
	f.ioMu.Lock()
	defer f.ioMu.Unlock()
	dir, ok := file.(fs.ReadDirFile)
	if !ok {
		return nil, errors.ErrUnsupported
	}
	entries, err := dir.ReadDir(1)
	if len(entries) != 0 {
		return entries[0], nil
	}
	return nil, err
}

// ReadAt, WriteAt, Append, Sync and Stat form the stream-facing retained-file
// contract. Each keeps offset operations serialized when the backing provider
// does not offer positional I/O itself. Append is deliberately capability-based
// because emulating it with seek/write is not atomic across descriptors.
func (f *retainedFile) ReadAt(p []byte, offset int64) (int, error) { return f.readAt(p, offset) }
func (f *retainedFile) WriteAt(p []byte, offset int64) (int, error) {
	return f.writeAt(p, offset)
}
func (f *retainedFile) Append(p []byte) (int, error) { return f.append(p) }
func (f *retainedFile) Sync() error                  { return f.sync() }
func (f *retainedFile) Stat() (fs.FileInfo, error)   { return f.stat() }

// descriptorResource represents a descriptor with retained host identity. fs
// supplies the optional descriptor-relative capability; the descriptor never
// retains a pathname to reopen later.
type descriptorResource struct {
	fs       fsapi.FS
	file     *retainedFile
	isDir    bool
	readable bool
	writable bool
	mutable  bool
	readOnly bool
	position int64
}

func newDescriptorResource(filesystem fsapi.FS, path string, isDir bool, readOnly bool) *descriptorResource {
	file, err := openDescriptorFile(filesystem, path, isDir, readOnly)
	if err != nil {
		// Keep the constructor compatible for tests and callers that need to
		// surface the open error later. Production preopens use the checked
		// constructor below and therefore never publish this unusable value.
		return &descriptorResource{fs: filesystem, isDir: isDir, readable: true, writable: !readOnly && !isDir, mutable: !readOnly && isDir, readOnly: readOnly}
	}
	return &descriptorResource{fs: filesystem, file: file, isDir: isDir, readable: true, writable: !readOnly && !isDir, mutable: !readOnly && isDir, readOnly: readOnly}
}

// newOpenedDescriptorResource takes ownership of file on entry, including when
// inspection fails. Callers must not close file after this function returns.
func newOpenedDescriptorResource(filesystem fsapi.FS, file fs.File, readable, writable, mutable bool) (*descriptorResource, error) {
	owner, err := newRetainedFile(file)
	if err != nil {
		return nil, err
	}
	info, err := owner.stat()
	if err != nil {
		owner.release()
		return nil, err
	}
	effectiveMutable := mutable && info.IsDir()
	return &descriptorResource{fs: filesystem, file: owner, isDir: info.IsDir(), readable: readable, writable: writable, mutable: effectiveMutable, readOnly: !writable && !effectiveMutable}, nil
}

func newPreopenDescriptorResource(filesystem fsapi.FS, path string, readOnly bool) (*descriptorResource, error) {
	owner, err := openDescriptorFile(filesystem, path, true, readOnly)
	if err != nil {
		return nil, err
	}
	info, err := owner.stat()
	if err != nil {
		owner.release()
		return nil, err
	}
	if !info.IsDir() {
		owner.release()
		return nil, fs.ErrInvalid
	}
	return &descriptorResource{fs: filesystem, file: owner, isDir: true, readable: true, mutable: !readOnly, readOnly: readOnly}, nil
}

func openDescriptorFile(filesystem fsapi.FS, path string, directory bool, readOnly bool) (*retainedFile, error) {
	var file fs.File
	var err error
	if directory {
		if opener, ok := filesystem.(interface {
			OpenDirectory(string, bool) (fs.File, error)
		}); ok {
			file, err = opener.OpenDirectory(path, false)
		} else {
			file, err = filesystem.Open(path)
		}
	} else {
		flags := os.O_RDONLY
		if !readOnly {
			flags = os.O_RDWR
		}
		file, err = filesystem.OpenFile(path, flags, 0)
	}
	if err != nil {
		return nil, err
	}
	return newRetainedFile(file)
}

func (d *descriptorResource) Type() preview2.ResourceType { return preview2.ResourceDescriptor }
func (d *descriptorResource) Drop() {
	if d.file != nil {
		d.file.release()
		d.file = nil
	}
}
