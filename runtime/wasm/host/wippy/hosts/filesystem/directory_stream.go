// SPDX-License-Identifier: MPL-2.0

package filesystem

import (
	"errors"
	"io"
	"io/fs"
	"sync"

	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

// directoryEntryStreamResource retains the directory file and asks it for one
// entry at a time. Listing a large host directory must not first allocate the
// whole listing in host memory.
type directoryEntryStreamResource struct {
	owner *retainedFile
	mu    sync.Mutex
	done  bool
}

func newDirectoryEntryStreamResource(desc *descriptorResource) (*directoryEntryStreamResource, error) {
	if desc == nil || !desc.isDir {
		return nil, fs.ErrInvalid
	}
	opener, ok := desc.fs.(fsapi.DescriptorOpener)
	if !ok {
		return nil, errors.ErrUnsupported
	}
	directory, release, descriptorErr := desc.borrowDirectory()
	if descriptorErr != nil {
		return nil, descriptorErr
	}
	defer release()
	file, openErr := opener.OpenDescriptorAt(directory, ".", fsapi.DescriptorOpenRequest{
		Read:      true,
		Directory: true,
		NoFollow:  true,
	})
	if openErr != nil {
		return nil, openErr
	}
	owner, ownerErr := newRetainedFile(file)
	if ownerErr != nil {
		_ = file.Close()
		return nil, ownerErr
	}
	return &directoryEntryStreamResource{owner: owner}, nil
}

func (*directoryEntryStreamResource) Type() preview2.ResourceType {
	return preview2.ResourceDirectoryEntryStream
}

func (s *directoryEntryStreamResource) Drop() {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.done = true
	owner := s.owner
	s.owner = nil
	s.mu.Unlock()
	owner.release()
}

func (s *directoryEntryStreamResource) readNext() (fs.DirEntry, error) {
	s.mu.Lock()
	if s.done || s.owner == nil {
		s.mu.Unlock()
		// Exhaustion is stable: preview2 callers may probe an entry stream more
		// than once after its final entry and must keep seeing no entry.
		return nil, nil
	}
	owner := s.owner
	if err := owner.retain(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()
	defer owner.release()

	entry, err := owner.readDirOne()
	if errors.Is(err, io.EOF) {
		s.Drop()
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return entry, nil
}
