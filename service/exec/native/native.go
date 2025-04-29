package native

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"

	execapi "github.com/ponyruntime/pony/api/service/exec"

	"go.uber.org/zap"
)

const (
	notStarted string = "not_started"
	running    string = "running"
	terminated string = "terminated"
)

// Executor implements the exec.ProcessExecutor interface
type Executor struct {
	log              *zap.Logger
	defaultEnv       map[string]string
	defaultWD        string
	commandWhitelist []string
}

// NewNativeExecutor creates a new native process executor
func NewNativeExecutor(log *zap.Logger, config *execapi.NativeExecutorConfig) *Executor {
	return &Executor{
		log:              log,
		defaultEnv:       config.DefaultEnv,
		defaultWD:        config.DefaultWorkDir,
		commandWhitelist: config.CommandWhitelist,
	}
}

// NewProcess implements exec.ProcessExecutor interface
func (e *Executor) NewProcess(cmd string, options execapi.ProcessOptions) (execapi.Process, error) {
	if len(e.commandWhitelist) > 0 {
		allowed := false
		for _, whitelistedCmd := range e.commandWhitelist {
			if cmd == whitelistedCmd {
				allowed = true
				break
			}
		}
		if !allowed {
			e.log.Warn("command rejected by whitelist", zap.String("command", cmd))
			return nil, fmt.Errorf("command not in whitelist: %s", cmd)
		}
	}

	// Merge default environment with provided environment
	env := make(map[string]string)
	for k, v := range e.defaultEnv {
		env[k] = v
	}
	for k, v := range options.Env {
		env[k] = v
	}

	// Use default working directory if not specified
	workDir := options.WorkDir
	if workDir == "" {
		workDir = e.defaultWD
	}

	// Create a new process executor with the given command and options
	return NewProcessExecutor(
		e.log,
		WithCmd(cmd),
		WithWorkingDir(workDir),
		WithEnv(env),
	), nil
}

// ProcessExecutor represents a native process implementation
type ProcessExecutor struct {
	rwm sync.RWMutex
	log *zap.Logger

	envs    map[string]string
	wd      string
	pid     int
	state   string
	command string
	stopped atomic.Pointer[bool]

	cmd *exec.Cmd

	stderrp   io.ReadCloser
	stdoutp   io.ReadCloser
	stdinPipe io.WriteCloser
}

// NewProcessExecutor creates a new process executor
func NewProcessExecutor(log *zap.Logger, opts ...Options) *ProcessExecutor {
	e := &ProcessExecutor{
		state: notStarted,
		log:   log,
	}

	e.stopped.Store(p(false))

	for _, opt := range opts {
		opt(e)
	}

	e.log.Debug("initializing command", zap.String("command", e.command))

	// G204: Subprocess launched with a potential tainted input or cmd arguments
	command := exec.Command("sh", "-c", e.command) //nolint:gosec
	if e.envs != nil {
		command.Env = os.Environ()
		for k, v := range e.envs {
			command.Env = append(command.Env, k+"="+v)
		}
	}

	if e.wd != "" {
		command.Dir = e.wd
	}

	// we can safely skip the error here
	// because we don't initialize stderrpipe twice or after the process was already started
	e.stderrp, _ = command.StderrPipe()

	// we can safely skip the error here
	// because we don't initialize stdoutpipe twice or after the process was already started
	e.stdoutp, _ = command.StdoutPipe()

	ip, _ := command.StdinPipe()

	e.stdinPipe = ip
	e.cmd = command

	return e
}

// Start implements exec.Process
func (e *ProcessExecutor) Start() error {
	e.rwm.Lock()
	defer e.rwm.Unlock()

	// execute command
	err := e.cmd.Start()
	if err != nil {
		e.stopped.Store(p(true))
		return err
	}

	e.pid = e.cmd.Process.Pid
	e.state = running
	return nil
}

// State returns the current state of the process
func (e *ProcessExecutor) State() string {
	e.rwm.RLock()
	defer e.rwm.RUnlock()

	return e.state
}

// WriteStdin implements exec.Process
func (e *ProcessExecutor) WriteStdin(data []byte) error {
	e.rwm.RLock()
	defer e.rwm.RUnlock()

	if e.state != running {
		e.log.Error("process is not running", zap.String("state", e.state))
		return errors.New("process is not running")
	}

	n, err := e.stdinPipe.Write(data)
	if err != nil {
		return err
	}

	e.log.Debug("written to stdin", zap.Int("bytes", n))

	return nil
}

// Signal implements exec.Process
func (e *ProcessExecutor) Signal(sig int) error {
	e.rwm.RLock()
	defer e.rwm.RUnlock()

	if e.state != running {
		e.log.Error("process is not running", zap.String("state", e.state))
		return errors.New("process is not running")
	}

	if e.pid <= 0 {
		e.log.Error("pid is not a positive int", zap.Int("pid", e.pid))
		return errors.New("pid is not a positive int, process is possibly not running")
	}

	// we're using os.FindProcess to avoid touching e.cmd
	pp, err := os.FindProcess(e.pid)
	if err != nil {
		e.log.Error("error finding process", zap.Error(err))
		return err
	}

	err = pp.Signal(syscall.Signal(sig))
	if err != nil {
		e.log.Error("error sending signal", zap.Error(err))
		return err
	}

	e.stopped.Store(p(true))
	return nil
}

// Stderr implements exec.Process
func (e *ProcessExecutor) Stderr() io.ReadCloser {
	e.rwm.RLock()
	defer e.rwm.RUnlock()

	return e.stderrp
}

// Stdout implements exec.Process
func (e *ProcessExecutor) Stdout() io.ReadCloser {
	e.rwm.RLock()
	defer e.rwm.RUnlock()

	return e.stdoutp
}

// StderrReader return stderr reader (for backward compatibility)
func (e *ProcessExecutor) StderrReader() io.ReadCloser {
	return e.Stderr()
}

// StdoutReader return stdout reader (for backward compatibility)
func (e *ProcessExecutor) StdoutReader() io.ReadCloser {
	return e.Stdout()
}

// Stop stops the process
func (e *ProcessExecutor) Stop() {
	e.rwm.Lock()
	defer e.rwm.Unlock()

	if e.pid <= 0 {
		e.log.Warn("pid is not a positive int", zap.Int("pid", e.pid))
		return
	}

	if *e.stopped.Load() {
		e.log.Warn("process already stopped")
		return
	}

	pp, err := os.FindProcess(e.pid)
	if err != nil {
		e.log.Error("error finding process", zap.Error(err))
		return
	}

	// kill the process
	_ = pp.Kill()
	// to prevent multiple calls to close()
	e.pid = 0
	e.state = terminated
	_ = e.stdoutp.Close()
	_ = e.stderrp.Close()
	e.stopped.Store(p(true))
}

// Wait implements exec.Process
func (e *ProcessExecutor) Wait() error {
	err := e.cmd.Wait()
	if err != nil {
		e.log.Error("command wait error", zap.Error(err))
	}

	e.rwm.Lock()
	e.state = terminated
	e.rwm.Unlock()

	e.stopped.Store(p(true))
	e.log.Debug("command finished")

	return err
}

func p[T any](val T) *T {
	return &val
}
