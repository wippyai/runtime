// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"errors"
	"io"
	"sync"
)

const terminalInputReadSize = 4 * 1024
const maxUnframedTerminalInput = 64 * 1024

// ErrInputFrameTooLarge reports an incomplete terminal sequence that exceeded
// the physical reader's retained-input budget.
var ErrInputFrameTooLarge = errors.New("terminal input framing limit exceeded")

// framedTerminalInput keeps the streaming decoder's retained input bounded.
// A complete event releases at most one decoder read. Ultraviolet may have one
// 4 KiB read queued behind that event, so an unterminated CSI or bracketed
// paste retains at most the 64 KiB budget plus one decoder read.
type framedTerminalInput struct {
	reader io.Reader
	err    error

	mu       sync.Mutex
	retained int
}

func newFramedTerminalInput(reader io.Reader) *framedTerminalInput {
	return &framedTerminalInput{reader: &deferInputReadError{reader: reader}}
}

func (r *framedTerminalInput) Read(p []byte) (int, error) {
	r.mu.Lock()
	remaining := maxUnframedTerminalInput - r.retained
	if remaining <= 0 {
		r.err = ErrInputFrameTooLarge
		r.mu.Unlock()
		return 0, ErrInputFrameTooLarge
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	r.mu.Unlock()

	n, err := r.reader.Read(p)
	if n > 0 {
		r.mu.Lock()
		r.retained += n
		r.mu.Unlock()
	}
	if err != nil {
		r.mu.Lock()
		r.err = err
		r.mu.Unlock()
	}
	return n, err
}

func (r *framedTerminalInput) acknowledgeEvent() {
	r.mu.Lock()
	r.retained -= min(r.retained, terminalInputReadSize)
	r.mu.Unlock()
}

func (r *framedTerminalInput) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// deferInputReadError makes a legal (n > 0, err != nil) read visible to the
// streaming decoder before reporting the terminal error on its next read.
type deferInputReadError struct {
	reader io.Reader
	err    error
}

func (r *deferInputReadError) Read(p []byte) (int, error) {
	if r.err != nil {
		err := r.err
		r.err = nil
		return 0, err
	}
	n, err := r.reader.Read(p)
	if n > 0 && err != nil {
		r.err = err
		return n, nil
	}
	return n, err
}
