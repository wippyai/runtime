package native

type Options func(*Executor)

func WithEnv(envs map[string]string) Options {
	return func(e *Executor) {
		e.envs = envs
	}
}

func WithWorkingDir(wd string) Options {
	return func(e *Executor) {
		e.wd = wd
	}
}

func WithCmd(cmd string) Options {
	return func(e *Executor) {
		e.command = cmd
	}
}
