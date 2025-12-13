package native

// todo: why separate file while otehrs not, need proper normalization

// Options defines function type for process executor configuration
type Options func(*ProcessExecutor)

// WithEnv sets environment variables for the process
func WithEnv(envs map[string]string) Options {
	return func(e *ProcessExecutor) {
		e.envs = envs
	}
}

// WithWorkingDir sets the working directory for the process
func WithWorkingDir(wd string) Options {
	return func(e *ProcessExecutor) {
		e.wd = wd
	}
}

// WithCmd sets the command to execute
func WithCmd(cmd string) Options {
	return func(e *ProcessExecutor) {
		e.command = cmd
	}
}
