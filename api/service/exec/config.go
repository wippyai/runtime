package exec

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
	return nil
}

// Validate validates the DockerExecutorConfig
func (c *DockerExecutorConfig) Validate() error {
	if c.Image == "" {
		return ErrImageRequired
	}
	return nil
}
