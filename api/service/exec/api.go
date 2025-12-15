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
	// Working directory for the process
	WorkDir string

	// Environment variables for the process
	Env map[string]string
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

	// Stdout returns a reader for the process stdout
	Stdout() io.ReadCloser

	// Stderr returns a reader for the process stderr
	Stderr() io.ReadCloser

	// Wait waits for the process to complete
	Wait() error
}
