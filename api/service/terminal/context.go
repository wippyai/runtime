// SPDX-License-Identifier: MPL-2.0

// Package terminal provides terminal service configuration.
package terminal

import (
	"context"
	"errors"
	"io"
	"sync"

	contextapi "github.com/wippyai/runtime/api/context"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

// terminalKey remains an internal compatibility alias while the canonical
// process-scoped key lives with the tty port contract.
var terminalKey = ttyapi.PortKey()

// Key returns the context key for terminal context.
func Key() *contextapi.Key {
	return terminalKey
}

// PipeContext holds the standard input/output/error streams for terminal operations.
type PipeContext struct {
	Raw           RawController
	Input         ttyapi.InputController
	Stdin         io.Reader
	Stdout        io.Writer
	Stderr        io.Writer
	activeSurface ttyapi.Surface
	Surface       func(ttyapi.SurfaceOptions) (ttyapi.Surface, error)
	Args          []string
	surfaceMu     sync.Mutex
	closed        bool
}

func (pc *PipeContext) InputController() ttyapi.InputController { return pc.Input }
func (pc *PipeContext) RawController() ttyapi.RawController     { return pc.Raw }
func (pc *PipeContext) Reader() io.Reader                       { return pc.Stdin }
func (pc *PipeContext) Output() io.Writer                       { return pc.Stdout }
func (pc *PipeContext) ErrorOutput() io.Writer                  { return pc.Stderr }
func (pc *PipeContext) OpenSurface(options ttyapi.SurfaceOptions) (ttyapi.Surface, error) {
	if pc == nil || pc.Surface == nil {
		return nil, ttyapi.ErrInvalidPort
	}
	pc.surfaceMu.Lock()
	defer pc.surfaceMu.Unlock()
	if pc.closed {
		return nil, ttyapi.ErrInvalidPort
	}
	if pc.activeSurface != nil {
		return nil, ttyapi.ErrSurfaceOpen
	}
	inner, err := pc.Surface(options)
	if err != nil {
		return nil, err
	}
	if inner == nil {
		return nil, ttyapi.ErrInvalidPort
	}
	leased := &leasedSurface{Surface: inner}
	leased.release = func() {
		pc.surfaceMu.Lock()
		if pc.activeSurface == leased {
			pc.activeSurface = nil
		}
		pc.surfaceMu.Unlock()
	}
	pc.activeSurface = leased
	return leased, nil
}

type leasedSurface struct {
	ttyapi.Surface
	closeErr error
	release  func()
	mu       sync.Mutex
	closed   bool
}

func (s *leasedSurface) Present(frame ttyapi.Frame) (ttyapi.PresentStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ttyapi.PresentStats{}, ttyapi.ErrInvalidPort
	}
	return s.Surface.Present(frame)
}

func (s *leasedSurface) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.Surface.Invalidate()
	}
}

func (s *leasedSurface) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	s.closeErr = s.Surface.Close()
	s.release()
	return s.closeErr
}

// RawController is retained as a compatibility alias.
type RawController = ttyapi.RawController

// NewTerminalContext creates a new terminal context with the provided input/output streams.
func NewTerminalContext(stdin io.Reader, stdout, stderr io.Writer) *PipeContext {
	return &PipeContext{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	}
}

// NewTerminalContextWithArgs creates a terminal context with args.
func NewTerminalContextWithArgs(stdin io.Reader, stdout, stderr io.Writer, args []string) *PipeContext {
	return &PipeContext{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Args:   args,
	}
}

// Close releases any terminal resources associated with the context.
func (pc *PipeContext) Close() error {
	if pc == nil {
		return nil
	}
	pc.surfaceMu.Lock()
	if pc.closed {
		pc.surfaceMu.Unlock()
		return nil
	}
	pc.closed = true
	surface := pc.activeSurface
	pc.surfaceMu.Unlock()
	var surfaceErr error
	if surface != nil {
		surfaceErr = surface.Close()
	}
	if pc.Raw == nil {
		return surfaceErr
	}
	return errors.Join(surfaceErr, pc.Raw.Reset())
}

// GetTerminalContext retrieves the terminal context from the given context if available.
func GetTerminalContext(ctx context.Context) *PipeContext {
	fc := contextapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(ttyapi.PortKey()); ok {
		if tc, ok := val.(*PipeContext); ok {
			return tc
		}
	}
	return nil
}

// WithTerminalContext sets the terminal context in the given context.
func WithTerminalContext(ctx context.Context, tc *PipeContext) error {
	fc := contextapi.FrameFromContext(ctx)
	if fc == nil {
		return contextapi.ErrNoFrameContext
	}
	return fc.Set(ttyapi.PortKey(), tc)
}
