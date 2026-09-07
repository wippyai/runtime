// SPDX-License-Identifier: MPL-2.0

package filesystem

import (
	"context"
	"errors"
	"io"
	"math"
	"sync"
	"syscall"

	"github.com/wippyai/wasm-runtime/memory/budget"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const fileStreamBufferBytes = 64 << 10

// fileStream owns one fixed buffer and one file lease. A dropped handle stops
// admission and wakes waiters immediately. Storage and its charge remain owned
// until the I/O worker exits; Drop never frees a buffer still used by a syscall.
type fileStream struct {
	owner       *retainedFile
	reservation *budget.Reservation
	err         error
	buf         []byte
	wake        chan struct{}
	stop        chan struct{}
	done        chan struct{}
	pollable    preview2.PollableResource
	offset      int64
	mu          sync.Mutex
	cleanup     sync.Once
	dropped     bool
	pumpDone    bool
}

func newFileStream(owner *retainedFile, offset uint64, buffers *preview2.HostBufferBudget) (*fileStream, error) {
	if owner == nil || offset > math.MaxInt64 {
		return nil, syscall.EINVAL
	}
	if err := owner.retain(); err != nil {
		return nil, err
	}
	info, err := owner.stat()
	if err != nil {
		owner.release()
		return nil, err
	}
	// Pipes/devices need a separately cancellable I/O capability. Do not open a
	// worker that can be trapped indefinitely by a guest-controlled special file.
	if !info.Mode().IsRegular() {
		owner.release()
		return nil, syscall.ENOTSUP
	}
	reservation, err := buffers.Reserve(fileStreamBufferBytes)
	if err != nil {
		owner.release()
		return nil, err
	}
	return &fileStream{owner: owner, reservation: reservation, buf: make([]byte, fileStreamBufferBytes), offset: int64(offset), wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}, nil
}
func (s *fileStream) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *fileStream) Drop() {
	s.mu.Lock()
	if s.dropped {
		s.mu.Unlock()
		return
	}
	s.dropped = true
	close(s.stop)
	s.pollable.Drop()
	finished := s.pumpDone
	s.mu.Unlock()
	if finished {
		s.release()
	}
}
func (s *fileStream) finish() {
	s.mu.Lock()
	s.pumpDone = true
	dropped := s.dropped
	s.mu.Unlock()
	if dropped {
		s.release()
	}
	close(s.done)
}
func (s *fileStream) release() {
	s.cleanup.Do(func() {
		s.mu.Lock()
		s.buf = nil
		s.mu.Unlock()
		s.owner.release()
		if s.reservation != nil {
			s.reservation.Release()
		}
	})
}
func (s *fileStream) Ready() bool                  { return s.pollable.Ready() }
func (s *fileStream) Subscribe() preview2.Pollable { return &fileStreamPollable{state: &s.pollable} }

type fileStreamPollable struct{ state *preview2.PollableResource }

func (*fileStreamPollable) Type() preview2.ResourceType { return preview2.ResourcePollable }
func (*fileStreamPollable) Drop()                       {}
func (p *fileStreamPollable) Ready() bool               { return p.state.Ready() }
func (p *fileStreamPollable) Notify() <-chan struct{}   { return p.state.Notify() }
func (p *fileStreamPollable) Block(ctx context.Context) { p.state.Block(ctx) }

func fileStreamError(err error) error {
	if errors.Is(err, io.EOF) {
		return &preview2.StreamError{Closed: true}
	}
	return &preview2.StreamError{LastOpFailed: true}
}

type fileInputStreamResource struct {
	*fileStream
	start int
	count int
}

func newFileInputStreamResource(owner *retainedFile, offset uint64, buffers *preview2.HostBufferBudget) (*fileInputStreamResource, error) {
	state, err := newFileStream(owner, offset, buffers)
	if err != nil {
		return nil, err
	}
	s := &fileInputStreamResource{fileStream: state}
	go s.pump()
	s.signal()
	return s, nil
}
func (*fileInputStreamResource) Type() preview2.ResourceType { return preview2.ResourceInputStream }
func (s *fileInputStreamResource) pump() {
	defer s.finish()
	for {
		select {
		case <-s.stop:
			return
		case <-s.wake:
		}
		s.mu.Lock()
		if s.dropped {
			s.mu.Unlock()
			return
		}
		if s.count != 0 {
			s.mu.Unlock()
			continue
		}
		buf := s.buf[:min(int64(len(s.buf)), math.MaxInt64-s.offset)]
		offset := s.offset
		s.mu.Unlock()
		n, err := 0, io.EOF
		if len(buf) > 0 {
			n, err = s.owner.readAt(buf, offset)
		}
		if n < 0 || n > len(buf) {
			n = 0
			err = io.ErrNoProgress
		}
		if n == 0 && err == nil {
			err = io.ErrNoProgress
		}
		s.mu.Lock()
		if s.dropped {
			s.mu.Unlock()
			return
		}
		s.start = 0
		s.count = n
		s.offset += int64(n)
		s.err = err
		s.pollable.SetReady(n > 0 || err != nil)
		s.mu.Unlock()
		if err != nil {
			return
		}
	}
}

// Read only copies already resident bytes; file I/O never runs on the guest worker.
func (s *fileInputStreamResource) Read(length uint64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dropped {
		return nil, &preview2.StreamError{Closed: true}
	}
	if s.count > 0 {
		n := int(min(length, uint64(s.count)))
		out := append([]byte(nil), s.buf[s.start:s.start+n]...)
		s.start += n
		s.count -= n
		if s.count == 0 {
			s.pollable.SetReady(s.err != nil)
			if s.err == nil {
				s.signal()
			}
		}
		return out, nil
	}
	if s.err != nil {
		return nil, fileStreamError(s.err)
	}
	return nil, nil
}

type fileOutputStreamResource struct {
	*fileStream
	count      int
	busy       bool
	flush      bool
	appendMode bool
}

func newFileOutputStreamResource(owner *retainedFile, offset uint64, appendMode bool, buffers *preview2.HostBufferBudget) (*fileOutputStreamResource, error) {
	if appendMode {
		if owner == nil {
			return nil, syscall.EINVAL
		}
		file, release, err := owner.borrow()
		if err != nil {
			return nil, err
		}
		_, supported := file.(interface{ Append([]byte) (int, error) })
		release()
		if !supported {
			return nil, errors.ErrUnsupported
		}
	}
	state, err := newFileStream(owner, offset, buffers)
	if err != nil {
		return nil, err
	}
	s := &fileOutputStreamResource{fileStream: state, appendMode: appendMode}
	s.pollable.SetReady(true)
	go s.pump()
	return s, nil
}
func (*fileOutputStreamResource) Type() preview2.ResourceType { return preview2.ResourceOutputStream }
func (s *fileOutputStreamResource) CheckWrite() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dropped {
		return 0, &preview2.StreamError{Closed: true}
	}
	if s.err != nil {
		return 0, fileStreamError(s.err)
	}
	if s.busy || s.count != 0 || s.flush {
		return 0, nil
	}
	return uint64(len(s.buf)), nil
}
func (s *fileOutputStreamResource) Write(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dropped {
		return &preview2.StreamError{Closed: true}
	}
	if s.err != nil {
		return fileStreamError(s.err)
	}
	if s.busy || s.count != 0 || s.flush || len(data) > len(s.buf) {
		return preview2.ErrWritePermit
	}
	if !s.appendMode && int64(len(data)) > math.MaxInt64-s.offset {
		return &preview2.StreamError{LastOpFailed: true}
	}
	if len(data) == 0 {
		return nil
	}
	copy(s.buf, data)
	s.count = len(data)
	s.pollable.SetReady(false)
	s.signal()
	return nil
}
func (s *fileOutputStreamResource) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dropped {
		return &preview2.StreamError{Closed: true}
	}
	if s.err != nil {
		return fileStreamError(s.err)
	}
	s.flush = true
	s.pollable.SetReady(false)
	s.signal()
	return nil
}
func (s *fileOutputStreamResource) pump() {
	defer s.finish()
	for {
		select {
		case <-s.stop:
			return
		case <-s.wake:
		}
		for {
			s.mu.Lock()
			if s.dropped {
				s.mu.Unlock()
				return
			}
			n := s.count
			flush := n == 0 && s.flush
			if n == 0 && !flush {
				s.mu.Unlock()
				break
			}
			buf := s.buf[:n]
			offset := s.offset
			s.busy = true
			if flush {
				s.flush = false
			}
			s.mu.Unlock()
			var err error
			written := 0
			if flush {
				err = s.owner.sync()
			} else if s.appendMode {
				written, err = s.owner.append(buf)
			} else {
				written, err = s.owner.writeAt(buf, offset)
			}
			if !flush && (written < 0 || written > n) {
				written = 0
				err = io.ErrShortWrite
			}
			if !flush && written != n && err == nil {
				err = io.ErrShortWrite
			}
			s.mu.Lock()
			s.busy = false
			if !flush {
				s.count = 0
				if !s.appendMode {
					s.offset += int64(written)
				}
			}
			s.err = err
			s.pollable.SetReady(s.dropped || err != nil || !s.flush)
			dropped := s.dropped
			s.mu.Unlock()
			if dropped || err != nil {
				return
			}
		}
	}
}
