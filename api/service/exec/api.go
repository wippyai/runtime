package exec

import (
	"io"

	"github.com/ponyruntime/pony/api/registry"
)

// Registry kind constants for executor types
const (
	// KindNativeExecutor identifies a native process executor
	KindNativeExecutor registry.Kind = "exec.native"
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

// NativeExecutorConfig defines configuration for native process execution
type NativeExecutorConfig struct {
	// Default working directory for processes
	DefaultWorkDir string `json:"default_work_dir"`

	// Default environment variables (always extended, never replaced)
	DefaultEnv map[string]string `json:"default_env"`

	// Command whitelist - if set, only commands in this list will be allowed
	CommandWhitelist []string `json:"command_whitelist"`
}
