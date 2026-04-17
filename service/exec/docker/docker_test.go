// SPDX-License-Identifier: MPL-2.0

package docker

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"go.uber.org/zap"
)

func skipIfNoDocker(t *testing.T) {
	if testing.Short() {
		t.Skip("Docker tests skipped in short mode")
	}
	if os.Getenv("SKIP_DOCKER_TESTS") == "1" {
		t.Skip("Docker tests disabled via SKIP_DOCKER_TESTS")
	}
	if os.Getenv("DOCKER_HOST") == "" {
		if _, err := os.Stat("/var/run/docker.sock"); os.IsNotExist(err) {
			t.Skip("Docker not available, skipping test")
		}
	}
}

func TestDockerExecutor_NewProcess(t *testing.T) {
	skipIfNoDocker(t)

	log := zap.NewNop()
	config := &execapi.DockerExecutorConfig{
		Image:      "alpine:latest",
		AutoRemove: true,
	}

	executor, err := NewDockerExecutor(log, config)
	require.NoError(t, err)
	defer func() { _ = executor.Close() }()

	proc, err := executor.NewProcess("echo hello", execapi.ProcessOptions{})
	require.NoError(t, err)
	assert.NotNil(t, proc)
}

func TestDockerExecutor_RequiresImage(t *testing.T) {
	log := zap.NewNop()
	config := &execapi.DockerExecutorConfig{}

	_, err := NewDockerExecutor(log, config)
	assert.ErrorIs(t, err, execapi.ErrImageRequired)
}

func TestDockerExecutor_Whitelist(t *testing.T) {
	skipIfNoDocker(t)

	log := zap.NewNop()
	config := &execapi.DockerExecutorConfig{
		Image:            "alpine:latest",
		AutoRemove:       true,
		CommandWhitelist: []string{"echo hello"},
	}

	executor, err := NewDockerExecutor(log, config)
	require.NoError(t, err)
	defer func() { _ = executor.Close() }()

	proc, err := executor.NewProcess("echo hello", execapi.ProcessOptions{})
	require.NoError(t, err)
	assert.NotNil(t, proc)

	_, err = executor.NewProcess("cat /etc/passwd", execapi.ProcessOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in whitelist")
}

func TestDockerProcess_EchoCommand(t *testing.T) {
	skipIfNoDocker(t)

	log := zap.NewNop()
	config := &execapi.DockerExecutorConfig{
		Image:      "alpine:latest",
		AutoRemove: true,
	}

	executor, err := NewDockerExecutor(log, config)
	require.NoError(t, err)
	defer func() { _ = executor.Close() }()

	proc, err := executor.NewProcess("echo hello world", execapi.ProcessOptions{})
	require.NoError(t, err)

	err = proc.Start()
	require.NoError(t, err)

	stdout := proc.Stdout()
	require.NotNil(t, stdout)

	output := make([]byte, 1024)
	n, err := stdout.Read(output)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("failed to read stdout: %v", err)
	}

	err = proc.Wait()
	require.NoError(t, err)

	assert.Contains(t, string(output[:n]), "hello world")
}

func TestDockerProcess_WithEnv(t *testing.T) {
	skipIfNoDocker(t)

	log := zap.NewNop()
	config := &execapi.DockerExecutorConfig{
		Image:      "alpine:latest",
		AutoRemove: true,
		DefaultEnv: map[string]string{"DEFAULT_VAR": "default_value"},
	}

	executor, err := NewDockerExecutor(log, config)
	require.NoError(t, err)
	defer func() { _ = executor.Close() }()

	proc, err := executor.NewProcess("sh -c 'echo $TEST_VAR $DEFAULT_VAR'", execapi.ProcessOptions{
		Env: map[string]string{"TEST_VAR": "test_value"},
	})
	require.NoError(t, err)

	err = proc.Start()
	require.NoError(t, err)

	stdout := proc.Stdout()
	output := make([]byte, 1024)
	n, _ := stdout.Read(output)

	err = proc.Wait()
	require.NoError(t, err)

	result := string(output[:n])
	assert.Contains(t, result, "test_value")
	assert.Contains(t, result, "default_value")
}

func TestDockerProcess_WorkDir(t *testing.T) {
	skipIfNoDocker(t)

	log := zap.NewNop()
	config := &execapi.DockerExecutorConfig{
		Image:      "alpine:latest",
		AutoRemove: true,
	}

	executor, err := NewDockerExecutor(log, config)
	require.NoError(t, err)
	defer func() { _ = executor.Close() }()

	proc, err := executor.NewProcess("pwd", execapi.ProcessOptions{
		WorkDir: "/tmp",
	})
	require.NoError(t, err)

	err = proc.Start()
	require.NoError(t, err)

	stdout := proc.Stdout()
	output := make([]byte, 1024)
	n, _ := stdout.Read(output)

	err = proc.Wait()
	require.NoError(t, err)

	assert.Contains(t, string(output[:n]), "/tmp")
}

func TestDockerProcess_Stderr(t *testing.T) {
	skipIfNoDocker(t)

	log := zap.NewNop()
	config := &execapi.DockerExecutorConfig{
		Image:      "alpine:latest",
		AutoRemove: true,
	}

	executor, err := NewDockerExecutor(log, config)
	require.NoError(t, err)
	defer func() { _ = executor.Close() }()

	proc, err := executor.NewProcess("sh -c 'echo error message >&2'", execapi.ProcessOptions{})
	require.NoError(t, err)

	err = proc.Start()
	require.NoError(t, err)

	stderr := proc.Stderr()
	require.NotNil(t, stderr)

	output := make([]byte, 1024)
	n, _ := stderr.Read(output)

	err = proc.Wait()
	require.NoError(t, err)

	assert.Contains(t, string(output[:n]), "error message")
}

func TestDockerProcess_ExitCode(t *testing.T) {
	skipIfNoDocker(t)

	log := zap.NewNop()
	config := &execapi.DockerExecutorConfig{
		Image:      "alpine:latest",
		AutoRemove: true,
	}

	executor, err := NewDockerExecutor(log, config)
	require.NoError(t, err)
	defer func() { _ = executor.Close() }()

	proc, err := executor.NewProcess("sh -c 'exit 42'", execapi.ProcessOptions{})
	require.NoError(t, err)

	err = proc.Start()
	require.NoError(t, err)

	err = proc.Wait()
	require.Error(t, err)

	var exitErr *ExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, 42, exitErr.ExitCode())
}

func TestDockerProcess_Signal(t *testing.T) {
	skipIfNoDocker(t)

	log := zap.NewNop()
	config := &execapi.DockerExecutorConfig{
		Image:      "alpine:latest",
		AutoRemove: true,
	}

	executor, err := NewDockerExecutor(log, config)
	require.NoError(t, err)
	defer func() { _ = executor.Close() }()

	proc, err := executor.NewProcess("sleep 60", execapi.ProcessOptions{})
	require.NoError(t, err)

	err = proc.Start()
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	// Use SIGKILL (9) to ensure non-zero exit
	err = proc.Signal(9)
	require.NoError(t, err)

	err = proc.Wait()
	assert.Error(t, err)

	var exitErr *ExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, 137, exitErr.ExitCode()) // 128 + 9 = SIGKILL
}

func TestDockerProcess_WriteStdin(t *testing.T) {
	skipIfNoDocker(t)

	log := zap.NewNop()
	config := &execapi.DockerExecutorConfig{
		Image:      "alpine:latest",
		AutoRemove: true,
	}

	executor, err := NewDockerExecutor(log, config)
	require.NoError(t, err)
	defer func() { _ = executor.Close() }()

	proc, err := executor.NewProcess("sh -c 'read line && echo $line'", execapi.ProcessOptions{})
	require.NoError(t, err)

	err = proc.Start()
	require.NoError(t, err)

	err = proc.WriteStdin([]byte("hello from stdin\n"))
	require.NoError(t, err)

	stdout := proc.Stdout()
	output := make([]byte, 1024)
	n, _ := stdout.Read(output)

	_ = proc.Wait()

	assert.Contains(t, string(output[:n]), "hello from stdin")
}

func TestDockerProcess_NotStarted(t *testing.T) {
	skipIfNoDocker(t)

	log := zap.NewNop()
	config := &execapi.DockerExecutorConfig{
		Image:      "alpine:latest",
		AutoRemove: true,
	}

	executor, err := NewDockerExecutor(log, config)
	require.NoError(t, err)
	defer func() { _ = executor.Close() }()

	proc, err := executor.NewProcess("echo test", execapi.ProcessOptions{})
	require.NoError(t, err)

	err = proc.Signal(15)
	assert.ErrorIs(t, err, ErrContainerNotStarted)

	err = proc.WriteStdin([]byte("test"))
	assert.ErrorIs(t, err, ErrContainerNotStarted)

	err = proc.Wait()
	assert.ErrorIs(t, err, ErrContainerNotStarted)
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{"   ", nil},
		{"echo hello", []string{"echo", "hello"}},
		{"echo 'hello world'", []string{"echo", "hello world"}},
		{"echo \"hello world\"", []string{"echo", "hello world"}},
		{"sh -c 'echo test'", []string{"sh", "-c", "echo test"}},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := parseCommand(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestExecutorFactory(t *testing.T) {
	log := zap.NewNop()
	factory := NewExecutorFactory(log)
	assert.NotNil(t, factory)
}

func TestExitError(t *testing.T) {
	err := &ExitError{Code: 42}
	assert.Equal(t, "container exited with code 42", err.Error())
	assert.Equal(t, 42, err.ExitCode())

	// Test Kind for normal exit codes
	assert.Equal(t, apierror.Internal, err.Kind())

	// Test Kind for SIGKILL (137)
	sigkillErr := &ExitError{Code: 137}
	assert.Equal(t, apierror.Canceled, sigkillErr.Kind())

	// Test Kind for SIGTERM (143)
	sigtermErr := &ExitError{Code: 143}
	assert.Equal(t, apierror.Canceled, sigtermErr.Kind())

	// Test Retryable
	assert.Equal(t, apierror.False, err.Retryable())

	// Test Details (lazy initialization)
	details := err.Details()
	assert.NotNil(t, details)
	exitCode, _ := details.Get("exit_code")
	assert.Equal(t, 42, exitCode)
}

func TestDockerError(t *testing.T) {
	// Test sentinel errors implement apierror.Error
	assert.Equal(t, apierror.Invalid, ErrContainerNotStarted.Kind())
	assert.Equal(t, apierror.False, ErrContainerNotStarted.Retryable())
	assert.Nil(t, ErrContainerNotStarted.Details())

	assert.Equal(t, apierror.AlreadyExists, ErrContainerAlreadyStart.Kind())
	assert.Equal(t, apierror.Invalid, ErrContainerStopped.Kind())
	assert.Equal(t, apierror.Unavailable, ErrStdinNotAvailable.Kind())
}

func TestNewCommandNotAllowedError(t *testing.T) {
	err := NewCommandNotAllowedError("rm -rf /")
	assert.Equal(t, apierror.PermissionDenied, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.Contains(t, err.Error(), "rm -rf /")
	assert.Contains(t, err.Error(), "not in whitelist")

	details := err.Details()
	assert.NotNil(t, details)
	cmd, _ := details.Get("command")
	assert.Equal(t, "rm -rf /", cmd)
}

func TestSignalName(t *testing.T) {
	assert.Equal(t, "SIGKILL", signalName(9))
	assert.Equal(t, "SIGTERM", signalName(15))
	assert.Equal(t, "99", signalName(99))
}

func TestDockerProcess_LongRunning(t *testing.T) {
	skipIfNoDocker(t)

	log := zap.NewNop()
	config := &execapi.DockerExecutorConfig{
		Image:      "alpine:latest",
		AutoRemove: true,
	}

	executor, err := NewDockerExecutor(log, config)
	require.NoError(t, err)
	defer func() { _ = executor.Close() }()

	proc, err := executor.NewProcess("sh -c 'for i in 1 2 3; do echo line$i; sleep 0.1; done'", execapi.ProcessOptions{})
	require.NoError(t, err)

	err = proc.Start()
	require.NoError(t, err)

	stdout := proc.Stdout()
	var sb strings.Builder
	buf := make([]byte, 1024)

	for {
		n, readErr := stdout.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			break
		}
	}

	err = proc.Wait()
	require.NoError(t, err)

	output := sb.String()
	assert.Contains(t, output, "line1")
	assert.Contains(t, output, "line2")
	assert.Contains(t, output, "line3")
}

func TestDockerProcess_SecurityOptions(t *testing.T) {
	skipIfNoDocker(t)

	log := zap.NewNop()
	config := &execapi.DockerExecutorConfig{
		Image:           "alpine:latest",
		AutoRemove:      true,
		NoNewPrivileges: true,
		CapDrop:         []string{"ALL"},
		CapAdd:          []string{"NET_BIND_SERVICE"},
		PidsLimit:       100,
		ReadOnlyRootfs:  true,
		Tmpfs:           map[string]string{"/tmp": "rw,noexec,nosuid,size=64m"},
	}

	executor, err := NewDockerExecutor(log, config)
	require.NoError(t, err)
	defer func() { _ = executor.Close() }()

	proc, err := executor.NewProcess("echo security test", execapi.ProcessOptions{})
	require.NoError(t, err)

	err = proc.Start()
	require.NoError(t, err)

	stdout := proc.Stdout()
	output := make([]byte, 1024)
	n, _ := stdout.Read(output)

	err = proc.Wait()
	require.NoError(t, err)

	assert.Contains(t, string(output[:n]), "security test")
}

func TestDockerProcess_MemoryLimit(t *testing.T) {
	skipIfNoDocker(t)

	log := zap.NewNop()
	config := &execapi.DockerExecutorConfig{
		Image:       "alpine:latest",
		AutoRemove:  true,
		MemoryLimit: 64 * 1024 * 1024, // 64MB
		CPUQuota:    50000,            // 0.5 CPU
	}

	executor, err := NewDockerExecutor(log, config)
	require.NoError(t, err)
	defer func() { _ = executor.Close() }()

	proc, err := executor.NewProcess("echo memory test", execapi.ProcessOptions{})
	require.NoError(t, err)

	err = proc.Start()
	require.NoError(t, err)

	stdout := proc.Stdout()
	output := make([]byte, 1024)
	n, _ := stdout.Read(output)

	err = proc.Wait()
	require.NoError(t, err)

	assert.Contains(t, string(output[:n]), "memory test")
}

func BenchmarkDockerProcess_StartAndWait(b *testing.B) {
	if os.Getenv("DOCKER_HOST") == "" && os.Getenv("CI") == "" {
		if _, err := os.Stat("/var/run/docker.sock"); os.IsNotExist(err) {
			b.Skip("Docker not available, skipping benchmark")
		}
	}

	log := zap.NewNop()
	config := &execapi.DockerExecutorConfig{
		Image:      "alpine:latest",
		AutoRemove: true,
	}

	executor, err := NewDockerExecutor(log, config)
	if err != nil {
		b.Fatalf("failed to create executor: %v", err)
	}
	defer func() { _ = executor.Close() }()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proc, err := executor.NewProcess("echo benchmark", execapi.ProcessOptions{})
		if err != nil {
			b.Fatalf("failed to create process: %v", err)
		}

		if err := proc.Start(); err != nil {
			b.Fatalf("failed to start process: %v", err)
		}

		stdout := proc.Stdout()
		buf := make([]byte, 256)
		_, _ = stdout.Read(buf)

		if err := proc.Wait(); err != nil {
			b.Fatalf("failed to wait for process: %v", err)
		}
	}
}
