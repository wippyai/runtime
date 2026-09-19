// SPDX-License-Identifier: MPL-2.0

package native

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/creack/pty"
	execapi "github.com/wippyai/runtime/api/service/exec"

	"go.uber.org/zap"
)

var (
	_ execapi.ProcessExecutor = (*Executor)(nil)
	_ execapi.Process         = (*ProcessExecutor)(nil)
	_ execapi.ProcessIdentity = (*ProcessExecutor)(nil)
	_ execapi.PTYProcess      = (*ptyProcess)(nil)
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
	processGroup     bool
}

// NewNativeExecutor creates a new native process executor
func NewNativeExecutor(log *zap.Logger, config *execapi.NativeExecutorConfig) *Executor {
	return &Executor{
		log:              log,
		defaultEnv:       config.DefaultEnv,
		defaultWD:        config.DefaultWorkDir,
		commandWhitelist: config.CommandWhitelist,
		processGroup:     config.ProcessGroup,
	}
}

// NewProcess implements exec.ProcessExecutor interface
func (e *Executor) NewProcess(cmd string, options execapi.ProcessOptions) (execapi.Process, error) {
	options, err := options.Clone()
	if err != nil {
		return nil, err
	}
	if len(options.Mounts) > 0 {
		return nil, execapi.ErrMountsUnsupported
	}
	if _, err := execapi.ParseCommand(cmd); err != nil {
		return nil, err
	}
	ptyOptions := options.PTY
	processGroup := e.processGroup
	if options.ProcessGroup != nil {
		processGroup = *options.ProcessGroup
	}
	if processGroup && !processGroupSupported {
		return nil, execapi.ErrProcessGroupUnsupported
	}
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
			return nil, NewCommandNotAllowedError(cmd)
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
	if ptyOptions != nil && ptyOptions.Term != "" {
		env["TERM"] = ptyOptions.Term
	}

	// Use default working directory if not specified
	workDir := options.WorkDir
	if workDir == "" {
		workDir = e.defaultWD
	}

	// Clean and validate working directory path
	if workDir != "" {
		workDir = filepath.Clean(workDir)
	}

	// Create a new process executor with the given command and options
	process := NewProcessExecutor(
		e.log,
		WithCmd(cmd),
		WithWorkingDir(workDir),
		WithEnv(env),
		WithPTY(ptyOptions),
		WithProcessGroup(processGroup),
	)
	if ptyOptions != nil {
		return &ptyProcess{ProcessExecutor: process}, nil
	}
	return process, nil
}

// ptyProcess is the capability-bearing view returned only for a process
// configured with a PTY. Ordinary pipe-backed processes do not accidentally
// satisfy exec.PTYProcess.
type ptyProcess struct{ *ProcessExecutor }

// ProcessExecutor represents a native process implementation
type ProcessExecutor struct {
	stderrp      io.ReadCloser
	stdoutp      io.ReadCloser
	stdoutw      *os.File
	stderrw      *os.File
	stdinPipe    io.WriteCloser
	cmd          *exec.Cmd
	log          *zap.Logger
	envs         map[string]string
	ptyMaster    *os.File
	pty          *execapi.PTYOptions
	wd           string
	state        string
	command      string
	pid          int
	pgid         int
	mu           sync.RWMutex
	ptyClose     sync.Once
	stopped      atomic.Bool
	stdoutOwned  bool
	stdinClosed  bool
	stderrOwned  bool
	processGroup bool
}

// NewProcessExecutor creates a new process executor
func NewProcessExecutor(log *zap.Logger, opts ...Option) *ProcessExecutor {
	e := &ProcessExecutor{
		state: notStarted,
		log:   log,
	}

	for _, opt := range opts {
		opt(e)
	}

	// Split command into executable and arguments
	cmdParts := parseCommand(e.command)
	if len(cmdParts) == 0 {
		cmdParts = []string{""}
	}

	// Create command with first part as executable and rest as arguments.
	// Stop()/Signal() still own lifecycle control.
	var command *exec.Cmd
	if len(cmdParts) > 1 {
		command = exec.CommandContext(context.Background(), cmdParts[0], cmdParts[1:]...) //nolint:gosec // G204: user-provided command.
	} else {
		command = exec.CommandContext(context.Background(), cmdParts[0]) //nolint:gosec // G204: user-provided command.
	}

	if e.envs != nil {
		// Use clean environment - only include explicitly configured variables
		// Do not inherit os.Environ() to prevent LD_PRELOAD, PATH hijacking
		names := make([]string, 0, len(e.envs))
		for name := range e.envs {
			names = append(names, name)
		}
		sort.Strings(names)
		command.Env = make([]string, 0, len(names))
		for _, name := range names {
			command.Env = append(command.Env, name+"="+e.envs[name])
		}
	}

	if e.wd != "" {
		command.Dir = e.wd
	}

	// A PTY child is started through pty.StartWithAttrs, which replaces
	// SysProcAttr with a session of its own; setsid already makes that child the
	// leader of a fresh process group, and adding setpgid on top of it fails.
	if e.processGroup && e.pty == nil {
		applyProcessGroup(command)
	}

	// The output pipes belong to the executor, not to os/exec. Cmd.Wait closes
	// the pipes StdoutPipe and StderrPipe create, which would discard whatever
	// the child wrote just before exiting the moment it is reaped, while the
	// caller still holds the reader. Handing Cmd a plain file leaves the
	// lifetime here, where the reader's owner decides it.
	if e.pty == nil {
		if reader, writer, err := os.Pipe(); err == nil {
			e.stderrp, e.stderrw = reader, writer
			command.Stderr = writer
		} else {
			e.log.Error("error creating stderr pipe", zap.Error(err))
		}

		if reader, writer, err := os.Pipe(); err == nil {
			e.stdoutp, e.stdoutw = reader, writer
			command.Stdout = writer
		} else {
			e.log.Error("error creating stdout pipe", zap.Error(err))
		}

		ip, _ := command.StdinPipe()
		e.stdinPipe = ip
	}
	e.cmd = command

	return e
}

// Start implements exec.Process
func (e *ProcessExecutor) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pty != nil {
		width, height, _ := e.pty.Dimensions()
		master, err := pty.StartWithSize(e.cmd, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
		if err != nil {
			e.releaseFailedStart()
			e.stopped.Store(true)
			if errors.Is(err, pty.ErrUnsupported) {
				return execapi.ErrPTYUnavailable.WithCause(err)
			}
			return err
		}
		e.ptyMaster = master
		e.stdinPipe, e.stdoutp = master, master
		e.stderrp = io.NopCloser(strings.NewReader(""))
	} else {
		err := e.cmd.Start()
		if err != nil {
			e.releaseFailedStart()
			e.stopped.Store(true)
			return err
		}
		// The child inherited its own descriptors for the write ends; keeping
		// the executor's copies open would hold the readers past the last real
		// writer and EOF would never arrive.
		e.releaseOutputWriters()
	}

	e.pid = e.cmd.Process.Pid
	// Both paths that give the child a group of its own make it the leader:
	// setpgid with a zero target group, and the PTY path's setsid. The group
	// identifier is therefore the child pid.
	if e.processGroup {
		e.pgid = e.pid
	}
	e.state = running
	return nil
}

// releaseOutputWriters closes the executor's copies of the output pipe write
// ends. The readers then end when the last process writing to them is gone,
// which is what distinguishes a closed pipe from a finished child.
func (e *ProcessExecutor) releaseOutputWriters() {
	if e.stdoutw != nil {
		_ = e.stdoutw.Close()
		e.stdoutw = nil
	}
	if e.stderrw != nil {
		_ = e.stderrw.Close()
		e.stderrw = nil
	}
}

func (e *ProcessExecutor) releaseFailedStart() {
	e.releaseOutputWriters()
	if e.stdinPipe != nil {
		_ = e.stdinPipe.Close()
		e.stdinPipe = nil
	}
	if e.stdoutp != nil {
		_ = e.stdoutp.Close()
		e.stdoutp = nil
	}
	if e.stderrp != nil {
		_ = e.stderrp.Close()
		e.stderrp = nil
	}
	e.stdinClosed = true
	e.state = terminated
}

// Pid implements exec.ProcessIdentity. It reports the identifier of the started
// child so callers can record which OS process an attempt ran as.
func (e *ProcessExecutor) Pid() (int, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.state == notStarted {
		return 0, ErrProcessNotStarted
	}
	if e.pid <= 0 {
		return 0, ErrInvalidPID
	}
	return e.pid, nil
}

func (p *ptyProcess) Resize(width, height int) error {
	if err := execapi.ValidatePTYSize(width, height); err != nil {
		return err
	}
	p.mu.RLock()
	master := p.ptyMaster
	p.mu.RUnlock()
	if master == nil {
		return execapi.ErrPTYUnavailable
	}
	return pty.Setsize(master, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
}

// State returns the current state of the process
func (e *ProcessExecutor) State() string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.state
}

// WriteStdin implements exec.Process
func (e *ProcessExecutor) WriteStdin(data []byte) error {
	e.mu.RLock()
	if e.state != running {
		state := e.state
		e.mu.RUnlock()
		e.log.Error("process is not running", zap.String("state", state))
		return ErrProcessNotRunning
	}
	if e.stdinClosed {
		e.mu.RUnlock()
		return ErrStdinClosed
	}
	stdin := e.stdinPipe
	e.mu.RUnlock()

	// Never hold the lifecycle lock across a potentially blocking OS write.
	// Stop/Signal must remain able to close or interrupt the process, which in
	// turn wakes this write with an error.
	n, err := stdin.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}

	e.log.Debug("written to stdin", zap.Int("bytes", n))

	return nil
}

// CloseStdin implements exec.StdinCloser: it ends the child's stdin so a
// reader that waits for end of file proceeds. The PTY master is the
// child's terminal, not a separate stdin, and stays open.
func (e *ProcessExecutor) CloseStdin() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != running {
		return ErrProcessNotRunning
	}
	if e.pty != nil {
		return ErrStdinPTY
	}
	if e.stdinClosed {
		return nil
	}
	e.stdinClosed = true
	if e.stdinPipe == nil {
		return nil
	}
	return e.stdinPipe.Close()
}

// Signal implements exec.Process
func (e *ProcessExecutor) Signal(sig int) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.state == notStarted {
		e.log.Error("process is not running", zap.String("state", e.state))
		return ErrProcessNotRunning
	}

	// A process group outlives the child that leads it: the descendants stay in
	// the group after the leader has been reaped, and the leader's identifier is
	// not handed to a new process while the group still has members. Addressing
	// the group therefore stays meaningful once the child's own exit has been
	// observed, which is when a supervisor most needs it. An empty group answers
	// ESRCH, and that is the state the caller is told about.
	if e.processGroup {
		if e.pgid <= 0 {
			e.log.Error("pgid is not a positive int", zap.Int("pgid", e.pgid))
			return ErrInvalidPID
		}
		if err := signalProcessGroup(e.pgid, syscall.Signal(sig)); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				e.log.Debug("process group already terminated")
				return errors.Join(ErrProcessNotRunning, os.ErrProcessDone)
			}
			e.log.Error("error sending signal to process group", zap.Error(err))
			return err
		}
		return nil
	}

	if e.state != running {
		e.log.Debug("process already terminated")
		return errors.Join(ErrProcessNotRunning, os.ErrProcessDone)
	}

	if e.pid <= 0 {
		e.log.Error("pid is not a positive int", zap.Int("pid", e.pid))
		return ErrInvalidPID
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

	return nil
}

// Stderr implements exec.Process
func (e *ProcessExecutor) Stderr() io.ReadCloser {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stderrp != nil {
		e.stderrOwned = true
	}

	return e.stderrp
}

// Stdout implements exec.Process
func (e *ProcessExecutor) Stdout() io.ReadCloser {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stdoutp != nil {
		e.stdoutOwned = true
	}

	return e.stdoutp
}

// Stop stops the process
func (e *ProcessExecutor) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.pid <= 0 {
		e.releaseFailedStart()
		e.closePTY()
		e.stopped.Store(true)
		return
	}

	if e.stopped.Load() {
		e.log.Warn("process already stopped")
		return
	}

	if e.processGroup {
		_ = signalProcessGroup(e.pgid, syscall.SIGKILL)
	} else {
		pp, err := os.FindProcess(e.pid)
		if err != nil {
			e.log.Error("error finding process", zap.Error(err))
			return
		}

		// kill the process
		_ = pp.Kill()
	}
	// to prevent multiple calls to close()
	e.pid = 0
	e.state = terminated
	if e.ptyMaster != nil {
		e.closePTY()
	} else {
		if e.stdoutp != nil {
			_ = e.stdoutp.Close()
		}
		if e.stderrp != nil {
			_ = e.stderrp.Close()
		}
	}
	e.stopped.Store(true)
}

// Wait implements exec.Process
func (e *ProcessExecutor) Wait() error {
	err := e.cmd.Wait()
	var nativeExit *exec.ExitError
	if errors.As(err, &nativeExit) {
		code := nativeExit.ExitCode()
		if status, statusOK := nativeExit.Sys().(syscall.WaitStatus); statusOK && status.Signaled() {
			code = 128 + int(status.Signal())
		}
		err = &ExitError{Code: code, cause: err}
	}
	var processExit *ExitError
	if err != nil && !errors.As(err, &processExit) {
		e.log.Error("command wait error", zap.Error(err))
	}

	e.mu.Lock()
	// Wait releases the output nobody asked for. Once Stdout or Stderr hands a
	// reader to a caller, that reader owns the final drain and close, so the
	// child's last bytes outlive the reap instead of being thrown away with it.
	if e.ptyMaster != nil {
		if !e.stdoutOwned {
			e.closePTY()
		}
	} else {
		if e.stdoutp != nil && !e.stdoutOwned {
			_ = e.stdoutp.Close()
		}
		if e.stderrp != nil && !e.stderrOwned {
			_ = e.stderrp.Close()
		}
	}
	e.state = terminated
	e.mu.Unlock()

	e.stopped.Store(true)
	if processExit != nil {
		e.log.Debug("command exited", zap.Int("exit_code", processExit.Code))
	} else {
		e.log.Debug("command finished")
	}

	return err
}

func (e *ProcessExecutor) closePTY() {
	e.ptyClose.Do(func() {
		if e.ptyMaster != nil {
			_ = e.ptyMaster.Close()
		}
	})
}

func parseCommand(cmd string) []string {
	parts, _ := execapi.ParseCommand(cmd)
	return parts
}
