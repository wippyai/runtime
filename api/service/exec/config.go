package exec

import (
	"github.com/ponyruntime/pony/api/registry"
)

// Registry kind constants for executor types
const (
	// KindNativeExecutor identifies a native process executor
	KindNativeExecutor registry.Kind = "exec.native"

	// KindDockerExecutor identifies a Docker process executor
	KindDockerExecutor registry.Kind = "exec.docker"
)

// NativeExecutorConfig defines configuration for native process execution
type NativeExecutorConfig struct {
	// Default working directory for processes
	DefaultWorkDir string `json:"default_work_dir"`

	// Default environment variables (always extended, never replaced)
	DefaultEnv map[string]string `json:"default_env"`
}

// DockerExecutorConfig defines configuration for Docker process execution
type DockerExecutorConfig struct {
	// Docker socket path
	Socket string `json:"socket"`

	// Default image to use
	DefaultImage string `json:"default_image"`

	// Default network
	Network string `json:"network"`

	// Default environment variables (always extended, never replaced)
	DefaultEnv map[string]string `json:"default_env"`
}
