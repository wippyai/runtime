package native

// Option defines function type for process executor configuration
type Option func(*ProcessExecutor)

// WithEnv sets environment variables for the process
func WithEnv(envs map[string]string) Option {
	return func(e *ProcessExecutor) {
		e.envs = envs
	}
}

// WithWorkingDir sets the working directory for the process
func WithWorkingDir(wd string) Option {
	return func(e *ProcessExecutor) {
		e.wd = wd
	}
}

// WithCmd sets the command to execute
func WithCmd(cmd string) Option {
	return func(e *ProcessExecutor) {
		e.command = cmd
	}
}
