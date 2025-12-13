// Package exec provides process execution service.
package exec

import (
	"io"

	"github.com/wippyai/runtime/api/registry"
)

// Registry kind constants for executor types
const (
	// KindNativeExecutor identifies a native process executor
	KindNativeExecutor registry.Kind = "exec.native"

	// KindDockerExecutor identifies a Docker container executor
	KindDockerExecutor registry.Kind = "exec.docker"
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

// DockerExecutorConfig defines configuration for Docker container execution
type DockerExecutorConfig struct {
	// Docker image to use for execution
	Image string `json:"image"`

	// Docker host URL (defaults to unix:///var/run/docker.sock)
	Host string `json:"host"`

	// Default working directory inside the container
	DefaultWorkDir string `json:"default_work_dir"`

	// Default environment variables (always extended, never replaced)
	DefaultEnv map[string]string `json:"default_env"`

	// Command whitelist - if set, only commands in this list will be allowed
	CommandWhitelist []string `json:"command_whitelist"`

	// Network mode for containers (e.g., "host", "bridge", "none")
	NetworkMode string `json:"network_mode"`

	// Volume mounts in format "host_path:container_path[:ro]"
	Volumes []string `json:"volumes"`

	// User to run as inside the container
	User string `json:"user"`

	// Memory limit in bytes (0 = no limit)
	MemoryLimit int64 `json:"memory_limit"`

	// CPU quota (0 = no limit, 100000 = 1 CPU)
	CPUQuota int64 `json:"cpu_quota"`

	// Remove container after exit
	AutoRemove bool `json:"auto_remove"`

	// Security options

	// ReadOnlyRootfs makes the container's root filesystem read-only
	ReadOnlyRootfs bool `json:"read_only_rootfs"`

	// NoNewPrivileges prevents privilege escalation via setuid/setgid
	NoNewPrivileges bool `json:"no_new_privileges"`

	// CapDrop specifies capabilities to drop (e.g., ["ALL"])
	CapDrop []string `json:"cap_drop"`

	// CapAdd specifies capabilities to add back after dropping
	CapAdd []string `json:"cap_add"`

	// PidsLimit limits the number of processes (0 = no limit)
	PidsLimit int64 `json:"pids_limit"`

	// Tmpfs mounts for writable directories when using read-only rootfs
	Tmpfs map[string]string `json:"tmpfs"`
}

// Validate validates the NativeExecutorConfig
func (c *NativeExecutorConfig) Validate() error {
	return nil
}

// Validate validates the DockerExecutorConfig
func (c *DockerExecutorConfig) Validate() error {
	if c.Image == "" {
		return ErrImageRequired
	}
	return nil
}
