// SPDX-License-Identifier: MPL-2.0

// Package exec provides process execution service.
package exec

import (
	"io"

	"github.com/wippyai/runtime/api/registry"
)

// Registry kind constants for executor types
const (
	// NativeExecutor identifies a native process executor
	NativeExecutor registry.Kind = "exec.native"

	// DockerExecutor identifies a Docker container executor
	DockerExecutor registry.Kind = "exec.docker"
)

// ProcessOptions defines options for creating a new process
type ProcessOptions struct {
	Env     map[string]string
	PTY     *PTYOptions
	WorkDir string
}

type PTYOptions struct {
	Term   string
	Width  int
	Height int
}

const (
	DefaultPTYWidth  = 80
	DefaultPTYHeight = 24
	MaxPTYDimension  = 65535
	MaxPTYCells      = 1 << 18
)

// ValidatePTYSize bounds both terminal coordinates and the backing screen.
func ValidatePTYSize(width, height int) error {
	if width < 1 || width > MaxPTYDimension || height < 1 || height > MaxPTYDimension ||
		height > MaxPTYCells/width {
		return ErrInvalidPTYSize
	}
	return nil
}

// Dimensions returns a validated initial terminal size. Zero values select
// the conventional 80x24 default.
func (o *PTYOptions) Dimensions() (int, int, error) {
	width, height := DefaultPTYWidth, DefaultPTYHeight
	if o == nil {
		return width, height, nil
	}
	if o.Width != 0 {
		width = o.Width
	}
	if o.Height != 0 {
		height = o.Height
	}
	if err := ValidatePTYSize(width, height); err != nil {
		return 0, 0, err
	}
	return width, height, nil
}

// ProcessExecutor defines the interface for process execution
type ProcessExecutor interface {
	// NewProcess creates a new process with the given command and options
	NewProcess(cmd string, options ProcessOptions) (Process, error)
}

// Process defines the interface for an executable process
type Process interface {
	// Start begins process execution
	Start() error

	// Signal sends a signal to the process
	Signal(sig int) error

	// WriteStdin writes data to the process stdin
	WriteStdin(data []byte) error

	// Stdout returns the process stdout reader. A caller that acquires a non-nil
	// reader owns its final drain and close.
	Stdout() io.ReadCloser

	// Stderr returns the process stderr reader. A caller that acquires a non-nil
	// reader owns its final drain and close.
	Stderr() io.ReadCloser

	// Wait waits for the process to complete
	Wait() error
}

// PTYProcess is the capability exposed only by PTY-backed processes.
type PTYProcess interface {
	Process
	Resize(width, height int) error
}

// WaitCanceler is an optional lifecycle capability for remote executors whose
// Wait operation can otherwise outlive an abandoned proxy or runtime process.
type WaitCanceler interface {
	CancelWait()
}
