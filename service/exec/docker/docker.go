// SPDX-License-Identifier: MPL-2.0

package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"go.uber.org/zap"
)

var (
	_ execapi.ProcessExecutor = (*Executor)(nil)
	_ execapi.Process         = (*Process)(nil)
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

	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if config.Host != "" {
		opts = append(opts, client.WithHost(config.Host))
	}

	cli, err := client.NewClientWithOpts(opts...)
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

	env := make([]string, 0, len(e.defaultEnv)+len(options.Env))
	for k, v := range e.defaultEnv {
		env = append(env, k+"="+v)
	}
	for k, v := range options.Env {
		env = append(env, k+"="+v)
	}

	workDir := options.WorkDir
	if workDir == "" {
		workDir = e.defaultWD
	}

	return &Process{
		log:             e.log,
		cli:             e.cli,
		image:           e.image,
		cmd:             parseCommand(cmd),
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
	}, nil
}

// Close closes the Docker client
func (e *Executor) Close() error {
	return e.cli.Close()
}

// Process represents a Docker container process
type Process struct {
	stdoutReader    io.ReadCloser
	waitErr         error
	stdinWriter     io.WriteCloser
	stderrReader    io.ReadCloser
	cli             *client.Client
	log             *zap.Logger
	tmpfs           map[string]string
	image           string
	containerID     string
	workDir         string
	networkMode     string
	user            string
	capAdd          []string
	capDrop         []string
	volumes         []string
	env             []string
	cmd             []string
	cpuQuota        int64
	memoryLimit     int64
	pidsLimit       int64
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

	ctx := context.Background()

	var mounts []mount.Mount
	for _, vol := range p.volumes {
		parts := strings.Split(vol, ":")
		if len(parts) >= 2 {
			m := mount.Mount{
				Type:   mount.TypeBind,
				Source: parts[0],
				Target: parts[1],
			}
			if len(parts) >= 3 && parts[2] == "ro" {
				m.ReadOnly = true
			}
			mounts = append(mounts, m)
		}
	}

	hostConfig := &container.HostConfig{
		AutoRemove:     p.autoRemove,
		Mounts:         mounts,
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
		Tty:          false,
	}

	resp, err := p.cli.ContainerCreate(ctx, config, hostConfig, &network.NetworkingConfig{}, nil, "")
	if err != nil {
		return NewContainerCreateError(err)
	}

	p.containerID = resp.ID
	p.log.Debug("container created", zap.String("id", p.containerID))

	attachResp, err := p.cli.ContainerAttach(ctx, p.containerID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		_ = p.cli.ContainerRemove(ctx, p.containerID, container.RemoveOptions{Force: true})
		return NewContainerAttachError(err)
	}

	p.stdinWriter = attachResp.Conn

	stdoutPipeR, stdoutPipeW := io.Pipe()
	stderrPipeR, stderrPipeW := io.Pipe()
	p.stdoutReader = stdoutPipeR
	p.stderrReader = stderrPipeR

	go func() {
		defer func() { _ = stdoutPipeW.Close() }()
		defer func() { _ = stderrPipeW.Close() }()
		_, err := stdcopy.StdCopy(stdoutPipeW, stderrPipeW, attachResp.Reader)
		if err != nil && !errors.Is(err, io.EOF) {
			p.log.Debug("stdcopy error", zap.Error(err))
		}
	}()

	if err := p.cli.ContainerStart(ctx, p.containerID, container.StartOptions{}); err != nil {
		attachResp.Close()
		_ = p.cli.ContainerRemove(ctx, p.containerID, container.RemoveOptions{Force: true})
		return NewContainerStartError(err)
	}

	p.started = true
	p.log.Debug("container started", zap.String("id", p.containerID))
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
		return ErrContainerStopped
	}
	containerID := p.containerID
	p.mu.RUnlock()

	sigName := signalName(sig)
	err := p.cli.ContainerKill(context.Background(), containerID, sigName)
	if err != nil {
		if strings.Contains(err.Error(), "is not running") {
			return ErrContainerStopped
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

	_, err := writer.Write(data)
	return err
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

	statusCh, errCh := p.cli.ContainerWait(context.Background(), containerID, container.WaitConditionNotRunning)

	var exitCode int64
	select {
	case err := <-errCh:
		if err != nil {
			p.mu.Lock()
			p.stopped = true
			p.waitErr = err
			p.mu.Unlock()
			return err
		}
	case status := <-statusCh:
		exitCode = status.StatusCode
		if status.Error != nil {
			p.mu.Lock()
			p.stopped = true
			p.waitErr = errors.New(status.Error.Message)
			p.mu.Unlock()
			return p.waitErr
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

// parseCommand splits a command string into parts
func parseCommand(cmd string) []string {
	if cmd == "" {
		return nil
	}

	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}

	// Estimate capacity: count spaces outside quotes
	estParts := 1 + strings.Count(cmd, " ")
	parts := make([]string, 0, estParts)

	var current strings.Builder
	current.Grow(len(cmd))

	inQuote := false
	quoteChar := rune(0)

	for _, c := range cmd {
		switch {
		case c == '"' || c == '\'':
			switch {
			case inQuote && c == quoteChar:
				inQuote = false
				quoteChar = 0
			case !inQuote:
				inQuote = true
				quoteChar = c
			default:
				current.WriteRune(c)
			}
		case c == ' ' && !inQuote:
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(c)
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
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
