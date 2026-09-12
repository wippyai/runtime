// SPDX-License-Identifier: MPL-2.0

// Package exec provides process execution service.
package exec

import (
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"

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
	Env map[string]string
	PTY *PTYOptions
	// ProcessGroup places the child in its own process group so that signals
	// addressed to the process reach its descendants as well. Nil selects the
	// executor default.
	ProcessGroup *bool
	WorkDir      string
	Mounts       []Mount
}

// Mount is a host path exposed at a path in a process. Mounts are bind mounts
// for executors that support them; the executor does not infer additional
// mounts from this value.
type Mount struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
}

// Validate checks a mount before it reaches an executor or security policy.
func (m Mount) Validate() error {
	if m.Source == "" {
		return fmt.Errorf("%w: source is required", ErrInvalidMount)
	}
	if strings.IndexByte(m.Source, 0) >= 0 {
		return fmt.Errorf("%w: source contains NUL", ErrInvalidMount)
	}
	if !filepath.IsAbs(m.Source) && !path.IsAbs(m.Source) {
		return fmt.Errorf("%w: source must be absolute", ErrInvalidMount)
	}
	if filepath.Clean(m.Source) != m.Source {
		return fmt.Errorf("%w: source must be a clean absolute path", ErrInvalidMount)
	}
	if m.Target == "" {
		return fmt.Errorf("%w: target is required", ErrInvalidMount)
	}
	if strings.IndexByte(m.Target, 0) >= 0 {
		return fmt.Errorf("%w: target contains NUL", ErrInvalidMount)
	}
	if !path.IsAbs(m.Target) {
		return fmt.Errorf("%w: target must be absolute", ErrInvalidMount)
	}
	return nil
}

// ValidateMounts validates each mount and rejects duplicate container targets.
func ValidateMounts(mounts []Mount) error {
	seen := make(map[string]struct{}, len(mounts))
	for _, mount := range mounts {
		if err := mount.Validate(); err != nil {
			return err
		}
		target := path.Clean(mount.Target)
		if _, exists := seen[target]; exists {
			return fmt.Errorf("%w: %q", ErrDuplicateMountTarget, target)
		}
		seen[target] = struct{}{}
	}
	return nil
}

// Clone validates and deep-copies process options. Executors retain the clone
// so callers cannot mutate a process after NewProcess returns.
func (o ProcessOptions) Clone() (ProcessOptions, error) {
	if err := o.Validate(); err != nil {
		return ProcessOptions{}, err
	}
	clone := o
	if o.Env != nil {
		clone.Env = make(map[string]string, len(o.Env))
		for name, value := range o.Env {
			clone.Env[name] = value
		}
	}
	if o.PTY != nil {
		pty := *o.PTY
		clone.PTY = &pty
	}
	if o.ProcessGroup != nil {
		group := *o.ProcessGroup
		clone.ProcessGroup = &group
	}
	if o.Mounts != nil {
		clone.Mounts = append([]Mount(nil), o.Mounts...)
	}
	return clone, nil
}

// Validate checks process options without retaining or mutating them.
func (o ProcessOptions) Validate() error {
	if err := ValidateMounts(o.Mounts); err != nil {
		return err
	}
	if o.PTY != nil {
		if _, _, err := o.PTY.Dimensions(); err != nil {
			return err
		}
	}
	return nil
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

// StdinCloser is the capability of a process whose stdin can be closed
// after the caller wrote everything, so a child that reads until end of
// file sees it. Closing is idempotent; later writes fail. A PTY-backed
// process has no separate stdin to close.
type StdinCloser interface {
	CloseStdin() error
}

// ProcessIdentity is an optional capability exposed by processes that carry an
// operating system process identifier on the host running the runtime.
type ProcessIdentity interface {
	// Pid returns the identifier of the started child process.
	Pid() (int, error)
}

// WaitCanceler is an optional lifecycle capability for remote executors whose
// Wait operation can otherwise outlive an abandoned proxy or runtime process.
type WaitCanceler interface {
	CancelWait()
}
