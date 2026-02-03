package terminal

import (
	"errors"
	"os"
	"sync"

	"golang.org/x/term"
)

var errNotTerminal = errors.New("stdin is not a terminal")

// RawManager controls terminal raw mode with reference counting.
type RawManager struct {
	file   *os.File
	state  *term.State
	mu     sync.Mutex
	refs   int
	active bool
}

// NewRawManager creates a RawManager for the provided terminal file.
func NewRawManager(file *os.File) *RawManager {
	return &RawManager{file: file}
}

// Enable puts the terminal into raw mode.
func (r *RawManager) Enable() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.file == nil || !term.IsTerminal(int(r.file.Fd())) {
		return errNotTerminal
	}
	if r.refs == 0 {
		state, err := term.MakeRaw(int(r.file.Fd()))
		if err != nil {
			return err
		}
		r.state = state
		r.active = true
	}
	r.refs++
	return nil
}

// Disable restores the terminal from raw mode when the refcount reaches zero.
func (r *RawManager) Disable() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.refs == 0 {
		return nil
	}
	r.refs--
	if r.refs > 0 {
		return nil
	}
	if r.state != nil && r.file != nil {
		err := term.Restore(int(r.file.Fd()), r.state)
		r.state = nil
		r.active = false
		return err
	}
	r.active = false
	return nil
}

// Reset forces raw mode off and clears refcount.
func (r *RawManager) Reset() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.refs = 0
	if r.state != nil && r.file != nil {
		err := term.Restore(int(r.file.Fd()), r.state)
		r.state = nil
		r.active = false
		return err
	}
	r.active = false
	return nil
}

// Enabled reports whether raw mode is currently active.
func (r *RawManager) Enabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}
