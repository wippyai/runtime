// SPDX-License-Identifier: MPL-2.0

package exec

import "fmt"

// NativeExecutorConfig defines configuration for native process execution
type NativeExecutorConfig struct {
	// Default environment variables (always extended, never replaced)
	DefaultEnv map[string]string `json:"default_env"`

	// Confine is the entry-owned authority ceiling for every launched process.
	Confine *Confinement `json:"confine,omitempty"`

	// Default working directory for processes
	DefaultWorkDir string `json:"default_work_dir"`

	// Command whitelist - if set, only commands in this list will be allowed
	CommandWhitelist []string `json:"command_whitelist"`

	// Start every process in its own process group so signals reach the whole
	// tree. A per-command process_group option overrides this default.
	ProcessGroup bool `json:"process_group"`
}

// DockerExecutorConfig defines configuration for Docker container execution
type DockerExecutorConfig struct {
	// LabelsFromEnv selects nonsecret per-process environment identities to
	// copy into container labels at creation. Every selected source is required.
	LabelsFromEnv    map[string]string `json:"labels_from_env,omitempty"`
	DefaultEnv       map[string]string `json:"default_env"`
	Tmpfs            map[string]string `json:"tmpfs"`
	Host             string            `json:"host"`
	DefaultWorkDir   string            `json:"default_work_dir"`
	NetworkMode      string            `json:"network_mode"`
	User             string            `json:"user"`
	Image            string            `json:"image"`
	CapDrop          []string          `json:"cap_drop"`
	CommandWhitelist []string          `json:"command_whitelist"`
	Volumes          []string          `json:"volumes"`
	CapAdd           []string          `json:"cap_add"`
	MemoryLimit      int64             `json:"memory_limit"`
	PidsLimit        int64             `json:"pids_limit"`
	CPUQuota         int64             `json:"cpu_quota"`
	NoNewPrivileges  bool              `json:"no_new_privileges"`
	ReadOnlyRootfs   bool              `json:"read_only_rootfs"`
	AutoRemove       bool              `json:"auto_remove"`
}

// Validate validates the NativeExecutorConfig
func (c *NativeExecutorConfig) Validate() error {
	if c.Confine != nil {
		if err := c.Confine.Validate(); err != nil {
			return err
		}
		if c.Confine.Env != nil {
			for name, value := range c.DefaultEnv {
				if !validConfinementEnvName(name) || containsNUL(value) ||
					!allowedConfinementEnvName(c.Confine.Env.Allow, name) ||
					pinnedConfinementEnvName(c.Confine.Env.Set, name) {
					return NewInvalidConfinementError("default_env")
				}
			}
		}
	}
	return nil
}

// Validate validates the DockerExecutorConfig
func (c *DockerExecutorConfig) Validate() error {
	if c.Image == "" {
		return ErrImageRequired
	}
	if len(c.LabelsFromEnv) > 64 {
		return fmt.Errorf("Docker ownership label mapping exceeds 64 entries")
	}
	for label, source := range c.LabelsFromEnv {
		if label == "" || len(label) > 256 || containsNUL(label) || !validConfinementEnvName(source) {
			return fmt.Errorf("invalid Docker ownership label mapping")
		}
	}
	return nil
}
