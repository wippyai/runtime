package native

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"go.uber.org/zap"
)

const (
	notStarted string = "not_started"
	running    string = "running"
	terminated string = "terminated"
)

type Executor struct {
	rwm sync.RWMutex
	log *zap.Logger

	envs    map[string]string
	wd      string
	pid     int
	state   string
	command string

	cmd *exec.Cmd

	stderrp   io.ReadCloser
	stdoutp   io.ReadCloser
	stdinPipe io.WriteCloser
}

func NewNativeExecutor(log *zap.Logger, opts ...Options) *Executor {
	e := &Executor{
		state: notStarted,
		log:   log,
	}

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

func (e *Executor) Start() error {
	e.rwm.Lock()
	defer e.rwm.Unlock()

	// execute command
	err := e.cmd.Start()
	if err != nil {
		return err
	}

	e.pid = e.cmd.Process.Pid
	e.state = running
	return nil
}

func (e *Executor) State() string {
	e.rwm.RLock()
	defer e.rwm.RUnlock()

	return e.state
}

func (e *Executor) WriteStdin(data []byte) error {
	e.rwm.RLock()
	defer e.rwm.RUnlock()

	if e.state != "running" {
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

func (e *Executor) Signal(sig int) {
	e.rwm.RLock()
	defer e.rwm.RUnlock()

	if e.state != running {
		e.log.Error("process is not running", zap.String("state", e.state))
		return
	}

	if e.pid <= 0 {
		e.log.Error("pid is not a positive int", zap.Int("pid", e.pid))
		return
	}

	// we're using os.FindProcess to avoid touching e.cmd
	p, err := os.FindProcess(e.pid)
	if err != nil {
		e.log.Error("error finding process", zap.Error(err))
		return
	}

	err = p.Signal(syscall.Signal(sig))
	if err != nil {
		e.log.Error("error sending signal", zap.Error(err))
		return
	}
}

func (e *Executor) StderrReader() io.ReadCloser {
	e.rwm.RLock()
	defer e.rwm.RUnlock()

	return e.stderrp
}

func (e *Executor) StdoutReader() io.ReadCloser {
	e.rwm.RLock()
	defer e.rwm.RUnlock()

	return e.stdoutp
}

func (e *Executor) Stop() {
	e.rwm.Lock()
	defer e.rwm.Unlock()

	if e.pid <= 0 {
		e.log.Error("pid is not a positive int", zap.Int("pid", e.pid))
		return
	}

	p, err := os.FindProcess(e.pid)
	if err != nil {
		e.log.Error("error finding process", zap.Error(err))
		return
	}

	// kill the process
	_ = p.Kill()
	// to prevent multiple calls to Stop()
	e.pid = 0
	e.state = terminated
	_ = e.stdoutp.Close()
	_ = e.stderrp.Close()
}

func (e *Executor) Wait() {
	err := e.cmd.Wait()
	if err != nil {
		e.log.Error("command wait error", zap.Error(err))
	}

	e.rwm.Lock()
	e.state = terminated
	e.rwm.Unlock()

	e.log.Debug("command finished")
}
