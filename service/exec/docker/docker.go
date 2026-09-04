// SPDX-License-Identifier: MPL-2.0

package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"go.uber.org/zap"
)

const (
	dockerStartTimeout   = 30 * time.Second
	dockerControlTimeout = 5 * time.Second
)

var (
	_ execapi.ProcessExecutor = (*Executor)(nil)
	_ execapi.Process         = (*Process)(nil)
	_ execapi.PTYProcess      = (*ptyProcess)(nil)
	_ io.Closer               = (*Executor)(nil)
)

// Executor implements exec.ProcessExecutor for Docker containers
type Executor struct {
	log              *zap.Logger
	cli              *client.Client
	tmpfs            map[string]string
	defaultEnv       map[string]string
	user             string
	defaultWD        string
	networkMode      string
	image            string
	capDrop          []string
	commandWhitelist []string
	capAdd           []string
	volumes          []string
	memoryLimit      int64
	cpuQuota         int64
	pidsLimit        int64
	autoRemove       bool
	readOnlyRootfs   bool
	noNewPrivileges  bool
}

// NewDockerExecutor creates a new Docker executor
func NewDockerExecutor(log *zap.Logger, config *execapi.DockerExecutorConfig) (*Executor, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	opts := []client.Opt{client.FromEnv}
	if config.Host != "" {
		opts = append(opts, client.WithHost(config.Host))
	}

	cli, err := client.New(opts...)
	if err != nil {
		return nil, NewDockerClientError(err)
	}

	return &Executor{
		log:              log,
		cli:              cli,
		image:            config.Image,
		defaultEnv:       config.DefaultEnv,
		defaultWD:        config.DefaultWorkDir,
		commandWhitelist: config.CommandWhitelist,
		networkMode:      config.NetworkMode,
		volumes:          config.Volumes,
		user:             config.User,
		memoryLimit:      config.MemoryLimit,
		cpuQuota:         config.CPUQuota,
		autoRemove:       config.AutoRemove,
		readOnlyRootfs:   config.ReadOnlyRootfs,
		noNewPrivileges:  config.NoNewPrivileges,
		capDrop:          config.CapDrop,
		capAdd:           config.CapAdd,
		pidsLimit:        config.PidsLimit,
		tmpfs:            config.Tmpfs,
	}, nil
}

// NewProcess creates a new container process
func (e *Executor) NewProcess(cmd string, options execapi.ProcessOptions) (execapi.Process, error) {
	command, err := execapi.ParseCommand(cmd)
	if err != nil {
		return nil, err
	}
	ptyOptions := options.PTY
	if ptyOptions != nil {
		copy := *ptyOptions
		ptyOptions = &copy
		if _, _, err := ptyOptions.Dimensions(); err != nil {
			return nil, err
		}
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

	term := ""
	if ptyOptions != nil && ptyOptions.Term != "" {
		term = ptyOptions.Term
	}
	env := mergeEnv(e.defaultEnv, options.Env, term)

	workDir := options.WorkDir
	if workDir == "" {
		workDir = e.defaultWD
	}

	process := &Process{
		log:             e.log,
		cli:             e.cli,
		image:           e.image,
		cmd:             command,
		env:             env,
		workDir:         workDir,
		networkMode:     e.networkMode,
		volumes:         e.volumes,
		user:            e.user,
		memoryLimit:     e.memoryLimit,
		cpuQuota:        e.cpuQuota,
		autoRemove:      e.autoRemove,
		readOnlyRootfs:  e.readOnlyRootfs,
		noNewPrivileges: e.noNewPrivileges,
		capDrop:         e.capDrop,
		capAdd:          e.capAdd,
		pidsLimit:       e.pidsLimit,
		tmpfs:           e.tmpfs,
		pty:             ptyOptions,
		startTimeout:    dockerStartTimeout,
		controlTimeout:  dockerControlTimeout,
	}
	process.waitCtx, process.cancelWait = context.WithCancel(context.Background())
	if ptyOptions != nil {
		return &ptyProcess{Process: process}, nil
	}
	return process, nil
}

// ptyProcess is returned only when the container was configured with a
// PTY, keeping resize a real capability rather than a boolean claim.
type ptyProcess struct{ *Process }

// Close closes the Docker client
func (e *Executor) Close() error {
	return e.cli.Close()
}

// Process represents a Docker container process
type Process struct {
	waitCtx         context.Context
	stdinWriter     io.WriteCloser
	stderrReader    io.ReadCloser
	stdoutReader    io.ReadCloser
	tmpfs           map[string]string
	log             *zap.Logger
	pty             *execapi.PTYOptions
	cancelWait      context.CancelFunc
	cli             *client.Client
	image           string
	containerID     string
	workDir         string
	networkMode     string
	user            string
	capDrop         []string
	volumes         []string
	cmd             []string
	capAdd          []string
	env             []string
	memoryLimit     int64
	pidsLimit       int64
	cpuQuota        int64
	startTimeout    time.Duration
	controlTimeout  time.Duration
	mu              sync.RWMutex
	stopped         bool
	started         bool
	noNewPrivileges bool
	readOnlyRootfs  bool
	autoRemove      bool
}

// Start creates and starts the container
func (p *Process) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return ErrContainerAlreadyStart
	}

	ctx, cancel := p.startContext()
	defer cancel()

	binds, err := buildBinds(p.volumes)
	if err != nil {
		return err
	}

	hostConfig := &container.HostConfig{
		AutoRemove:     p.autoRemove,
		Binds:          binds,
		ReadonlyRootfs: p.readOnlyRootfs,
		Tmpfs:          p.tmpfs,
		CapDrop:        p.capDrop,
		CapAdd:         p.capAdd,
		Resources: container.Resources{
			Memory:    p.memoryLimit,
			CPUQuota:  p.cpuQuota,
			PidsLimit: pidsLimitPtr(p.pidsLimit),
		},
		SecurityOpt: buildSecurityOpts(p.noNewPrivileges),
	}
	if p.pty != nil {
		width, height, _ := p.pty.Dimensions()
		hostConfig.ConsoleSize = [2]uint{uint(height), uint(width)}
	}

	if p.networkMode != "" {
		hostConfig.NetworkMode = container.NetworkMode(p.networkMode)
	}

	config := &container.Config{
		Image:        p.image,
		Cmd:          p.cmd,
		Env:          p.env,
		WorkingDir:   p.workDir,
		User:         p.user,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		OpenStdin:    true,
		StdinOnce:    false,
		Tty:          p.pty != nil,
	}

	resp, err := p.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           config,
		HostConfig:       hostConfig,
		NetworkingConfig: &network.NetworkingConfig{},
	})
	if err != nil {
		return NewContainerCreateError(err)
	}

	p.containerID = resp.ID
	p.log.Debug("container created", zap.String("id", p.containerID))
	committed := false
	defer func() {
		if committed {
			return
		}
		cleanupCtx, cleanupCancel := p.controlContext()
		defer cleanupCancel()
		_, _ = p.cli.ContainerRemove(cleanupCtx, resp.ID, client.ContainerRemoveOptions{Force: true})
		p.containerID = ""
	}()

	attachResp, err := p.cli.ContainerAttach(ctx, p.containerID, client.ContainerAttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return NewContainerAttachError(err)
	}
	defer func() {
		if !committed {
			attachResp.Close()
		}
	}()

	if _, err := p.cli.ContainerStart(ctx, p.containerID, client.ContainerStartOptions{}); err != nil {
		return NewContainerStartError(err)
	}
	if p.pty != nil {
		width, height, _ := p.pty.Dimensions()
		if _, err := p.cli.ContainerResize(ctx, p.containerID, client.ContainerResizeOptions{Width: uint(width), Height: uint(height)}); err != nil {
			return NewContainerResizeError(err)
		}
	}

	stdoutPipeR, stdoutPipeW := io.Pipe()
	stderrPipeR, stderrPipeW := io.Pipe()
	p.stdinWriter = attachResp.Conn
	p.stdoutReader = stdoutPipeR
	p.stderrReader = stderrPipeR
	p.started = true
	committed = true
	go p.copyAttachedOutput(attachResp, stdoutPipeW, stderrPipeW)
	p.log.Debug("container started", zap.String("id", p.containerID))
	return nil
}

func (p *Process) copyAttachedOutput(attach client.ContainerAttachResult, stdout, stderr *io.PipeWriter) {
	defer attach.Close()
	defer func() { _ = stdout.Close() }()
	defer func() { _ = stderr.Close() }()
	var err error
	if p.pty != nil {
		_, err = io.Copy(stdout, attach.Reader)
	} else {
		_, err = stdcopy.StdCopy(stdout, stderr, attach.Reader)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		p.log.Debug("docker output copy failed", zap.Error(err))
	}
}

func (p *ptyProcess) Resize(width, height int) error {
	if err := execapi.ValidatePTYSize(width, height); err != nil {
		return err
	}
	p.mu.RLock()
	if !p.started || p.stopped || p.pty == nil {
		p.mu.RUnlock()
		return execapi.ErrPTYUnavailable
	}
	id := p.containerID
	p.mu.RUnlock()
	ctx, cancel := p.controlContext()
	defer cancel()
	_, err := p.cli.ContainerResize(ctx, id, client.ContainerResizeOptions{Width: uint(width), Height: uint(height)})
	if err != nil {
		return NewContainerResizeError(err)
	}
	return nil
}

// Signal sends a signal to the container
func (p *Process) Signal(sig int) error {
	p.mu.RLock()
	if !p.started {
		p.mu.RUnlock()
		return ErrContainerNotStarted
	}
	if p.stopped {
		p.mu.RUnlock()
		return errors.Join(ErrContainerStopped, os.ErrProcessDone)
	}
	containerID := p.containerID
	p.mu.RUnlock()

	sigName := signalName(sig)
	ctx, cancel := p.controlContext()
	defer cancel()
	_, err := p.cli.ContainerKill(ctx, containerID, client.ContainerKillOptions{Signal: sigName})
	if err != nil {
		if strings.Contains(err.Error(), "is not running") {
			return errors.Join(ErrContainerStopped, os.ErrProcessDone)
		}
		return NewSignalError(err)
	}

	p.log.Debug("signal sent", zap.String("id", containerID), zap.String("signal", sigName))
	return nil
}

// WriteStdin writes data to the container's stdin
func (p *Process) WriteStdin(data []byte) error {
	p.mu.RLock()
	if !p.started {
		p.mu.RUnlock()
		return ErrContainerNotStarted
	}
	if p.stopped {
		p.mu.RUnlock()
		return ErrContainerStopped
	}
	writer := p.stdinWriter
	p.mu.RUnlock()

	if writer == nil {
		return ErrStdinNotAvailable
	}

	written, err := writer.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

// Stdout returns a reader for the container's stdout
func (p *Process) Stdout() io.ReadCloser {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.stdoutReader
}

// Stderr returns a reader for the container's stderr
func (p *Process) Stderr() io.ReadCloser {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.stderrReader
}

// Wait waits for the container to exit and returns any error
func (p *Process) Wait() error {
	p.mu.RLock()
	if !p.started {
		p.mu.RUnlock()
		return ErrContainerNotStarted
	}
	containerID := p.containerID
	p.mu.RUnlock()

	waitCtx := p.waitCtx
	if waitCtx == nil {
		waitCtx = context.Background()
	}
	defer p.CancelWait()
	waitResult := p.cli.ContainerWait(waitCtx, containerID, client.ContainerWaitOptions{
		Condition: container.WaitConditionNotRunning,
	})
	statusCh := waitResult.Result
	errCh := waitResult.Error

	var exitCode int64
	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case status := <-statusCh:
		exitCode = status.StatusCode
		if status.Error != nil {
			waitErr := errors.New(status.Error.Message)
			p.mu.Lock()
			p.stopped = true
			p.mu.Unlock()
			return waitErr
		}
	}

	p.mu.Lock()
	p.stopped = true
	p.mu.Unlock()

	p.log.Debug("container exited", zap.String("id", containerID), zap.Int64("exit_code", exitCode))

	if exitCode != 0 {
		return &ExitError{Code: int(exitCode)}
	}

	return nil
}

// CancelWait releases a ContainerWait request when a higher-level lifecycle
// owner has abandoned the session after bounded graceful and forced shutdown.
func (p *Process) CancelWait() {
	if p.cancelWait != nil {
		p.cancelWait()
	}
}

func (p *Process) startContext() (context.Context, context.CancelFunc) {
	timeout := p.startTimeout
	if timeout <= 0 {
		timeout = dockerStartTimeout
	}
	return context.WithTimeout(context.Background(), timeout)
}

func (p *Process) controlContext() (context.Context, context.CancelFunc) {
	timeout := p.controlTimeout
	if timeout <= 0 {
		timeout = dockerControlTimeout
	}
	return context.WithTimeout(context.Background(), timeout)
}

// buildBinds prepares Docker short-syntax volume specifications for the daemon.
// Relative host paths are resolved client-side, matching Docker CLI behavior;
// the daemon remains the authority for parsing and validating the full syntax.
func buildBinds(volumes []string) ([]string, error) {
	if len(volumes) == 0 {
		return nil, nil
	}

	binds := make([]string, 0, len(volumes))
	for _, volume := range volumes {
		source, remainder, ok := strings.Cut(volume, ":")
		if !ok {
			// Preserve unsupported or malformed forms so the Docker daemon,
			// which owns bind syntax, can return the authoritative error.
			binds = append(binds, volume)
			continue
		}

		if !filepath.IsAbs(source) && isExplicitRelativePath(source) {
			absolute, err := filepath.Abs(source)
			if err != nil {
				return nil, fmt.Errorf("resolve docker volume source %q: %w", source, err)
			}
			volume = absolute + ":" + remainder
		}
		binds = append(binds, volume)
	}
	return binds, nil
}

func isExplicitRelativePath(source string) bool {
	if source == "." || source == ".." {
		return true
	}
	return strings.HasPrefix(source, ".") && strings.ContainsAny(source, `/\`)
}

func mergeEnv(defaults, overrides map[string]string, term string) []string {
	merged := make(map[string]string, len(defaults)+len(overrides)+1)
	for name, value := range defaults {
		merged[name] = value
	}
	for name, value := range overrides {
		merged[name] = value
	}
	if term != "" {
		merged["TERM"] = term
	}

	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}
	sort.Strings(names)
	env := make([]string, 0, len(names))
	for _, name := range names {
		env = append(env, name+"="+merged[name])
	}
	return env
}

var signalNames = map[int]string{
	1:  "SIGHUP",
	2:  "SIGINT",
	3:  "SIGQUIT",
	6:  "SIGABRT",
	9:  "SIGKILL",
	14: "SIGALRM",
	15: "SIGTERM",
}

func signalName(sig int) string {
	if name, ok := signalNames[sig]; ok {
		return name
	}
	return fmt.Sprintf("%d", sig)
}

func buildSecurityOpts(noNewPrivileges bool) []string {
	if noNewPrivileges {
		return []string{"no-new-privileges:true"}
	}
	return nil
}

func pidsLimitPtr(limit int64) *int64 {
	if limit == 0 {
		return nil
	}
	return &limit
}
