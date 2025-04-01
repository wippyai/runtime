package native

import (
	"errors"
	"io"
	"io/fs"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/service/exec"
	mocklogger "github.com/ponyruntime/pony/tests/mock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestExecutor_Execute(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{
			name:    "echo command",
			command: "echo 'hello world'",
			wantErr: false,
		},
		{
			name:    "invalid command",
			command: "invalidcommand",
			wantErr: false, // execute() doesn't return error for invalid commands
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, _ := zap.NewDevelopment()

			// Create the process
			nativeExecutor := NewNativeExecutor(logger, &exec.NativeExecutorConfig{})
			process, err := nativeExecutor.NewProcess(tt.command, exec.ProcessOptions{})
			assert.NoError(t, err)

			// Start the process
			err = process.Start()

			go func() {
				process.Wait()
			}()

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Stop the process
			processExecutor, ok := process.(*ProcessExecutor)
			assert.True(t, ok)
			processExecutor.Stop()
		})
	}
}

func TestExecutor_MegaCommand(t *testing.T) {
	logger := zap.NewNop()

	// Create the process
	nativeExecutor := NewNativeExecutor(logger, &exec.NativeExecutorConfig{})
	process, err := nativeExecutor.NewProcess("cat /dev/urandom | hexdump -C", exec.ProcessOptions{})
	assert.NoError(t, err)

	processExecutor, ok := process.(*ProcessExecutor)
	assert.True(t, ok)

	err = process.Start()
	assert.NoError(t, err)

	go func() {
		process.Wait()
	}()

	go func() {
		time.Sleep(time.Second * 5)
		processExecutor.Stop()
	}()

	sb := new(strings.Builder)
	for {
		// we don't care about the perf here
		buf := make([]byte, 65536)
		_, err = process.Stdout().Read(buf)
		if err != nil {
			// fs.ErrClosed is returned when the process is stopped (the file is already closed)
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, fs.ErrClosed) {
				break
			}

			t.Fatal(err)
		}

		sb.Write(buf)
	}

	if sb.Len() == 0 {
		t.Fatal("no output")
	}
}

func TestExecutor_Stdout(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// Create the process
	nativeExecutor := NewNativeExecutor(logger, &exec.NativeExecutorConfig{})
	process, err := nativeExecutor.NewProcess("sleep 1 && echo 'hello world'", exec.ProcessOptions{})
	assert.NoError(t, err)

	processExecutor, ok := process.(*ProcessExecutor)
	assert.True(t, ok)

	err = process.Start()
	assert.NoError(t, err)

	go func() {
		process.Wait()
		assert.Equal(t, "terminated", processExecutor.State())
	}()

	assert.Equal(t, "running", processExecutor.State())

	sb := new(strings.Builder)

	for {
		// we don't care about the perf here
		buf := make([]byte, 65536)
		_, err = process.Stdout().Read(buf)
		if err != nil {
			// fs.ErrClosed is returned when the process is stopped (the file is already closed)
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, fs.ErrClosed) {
				break
			}
			t.Fatal(err)
		}

		sb.Write(buf)
	}

	assert.Contains(t, sb.String(), "hello world")
}

func TestExecutor_EmptyCmd(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// Create the process
	nativeExecutor := NewNativeExecutor(logger, &exec.NativeExecutorConfig{})
	process, err := nativeExecutor.NewProcess("", exec.ProcessOptions{})
	assert.NoError(t, err)

	err = process.Start()
	assert.NoError(t, err)

	go func() {
		process.Wait()
	}()

	sb := new(strings.Builder)

	for {
		// we don't care about the perf here
		buf := make([]byte, 65536)
		_, err = process.Stderr().Read(buf)
		if err != nil {
			// fs.ErrClosed is returned when the process is stopped (the file is already closed)
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, fs.ErrClosed) {
				break
			}
			t.Fatal(err)
		}

		sb.Write(buf)
	}

	assert.Contains(t, sb.String(), "")
}

func TestExecutor_Stderr(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// Create the process
	nativeExecutor := NewNativeExecutor(logger, &exec.NativeExecutorConfig{})
	process, err := nativeExecutor.NewProcess("sleep 1 && echo 'error message' >&2", exec.ProcessOptions{})
	assert.NoError(t, err)

	err = process.Start()
	assert.NoError(t, err)

	go func() {
		process.Wait()
	}()

	sb := new(strings.Builder)

	for {
		// we don't care about the perf here
		buf := make([]byte, 65536)
		_, err = process.Stderr().Read(buf)
		if err != nil {
			// fs.ErrClosed is returned when the process is stopped (the file is already closed)
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, fs.ErrClosed) {
				sb.Write(buf)
				break
			}

			t.Fatal(err)
		}

		sb.Write(buf)
	}

	assert.Contains(t, sb.String(), "error message")
}

func TestExecutor_ReadWithInvalidCommand(t *testing.T) {
	l, oLogger := mocklogger.ZapTestLogger(zap.DebugLevel)

	// Create the process
	nativeExecutor := NewNativeExecutor(l, &exec.NativeExecutorConfig{})
	process, err := nativeExecutor.NewProcess("sleep 1 && invalidcommand", exec.ProcessOptions{})
	assert.NoError(t, err)

	err = process.Start()
	assert.NoError(t, err)

	go func() {
		process.Wait()
	}()

	// Wait for an error message in stderr
	sb := new(strings.Builder)

	for {
		// we don't care about the perf here
		buf := make([]byte, 65536)
		time.Sleep(time.Second)
		_, err = process.Stderr().Read(buf)
		if err != nil {
			// fs.ErrClosed is returned when the process is stopped (the file is already closed)
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, fs.ErrClosed) {
				break
			}

			t.Fatal(err)
		}

		sb.Write(buf)
	}

	if runtime.GOOS == "linux" {
		assert.Equal(t, 1, oLogger.FilterMessageSnippet("command wait error").Len())
	} else {
		// macOS
		assert.Contains(t, sb.String(), "sh: invalidcommand: command not found")
	}
}

func TestExecutor_WriteStdin(t *testing.T) {
	tests := []struct {
		name    string
		command string
		input   string
		expect  string
		wantErr bool
	}{
		{
			name:    "write to cat command",
			command: "cat",
			input:   "hello world",
			expect:  "hello world",
			wantErr: false,
		},
		{
			name:    "write to non-running process",
			command: "echo 'test'",
			input:   "hello world",
			expect:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, _ := zap.NewDevelopment()

			// Create the process
			nativeExecutor := NewNativeExecutor(logger, &exec.NativeExecutorConfig{})
			process, err := nativeExecutor.NewProcess(tt.command, exec.ProcessOptions{})
			assert.NoError(t, err)

			processExecutor, ok := process.(*ProcessExecutor)
			assert.True(t, ok)

			if tt.command == "cat" {
				err := process.Start()
				assert.NoError(t, err)

				go func() {
					process.Wait()
				}()

				go func() {
					err2 := process.WriteStdin([]byte(tt.input))
					assert.NoError(t, err2)
				}()

				sb := new(strings.Builder)

				for {
					// we don't care about the perf here
					buf := make([]byte, 65536)
					_, err = process.Stdout().Read(buf)
					if err != nil {
						// fs.ErrClosed is returned when the process is stopped (the file is already closed)
						if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, fs.ErrClosed) {
							break
						}

						t.Fatal(err)
					}

					sb.Write(buf)
					processExecutor.Stop()
				}

				assert.Contains(t, sb.String(), tt.expect)
			} else {
				// Test writing to a non-running process
				err := process.WriteStdin([]byte(tt.input))
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "process is not running")
			}
		})
	}
}

func TestNativeExecutor_Config(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// Test with custom environment and working directory
	config := &exec.NativeExecutorConfig{
		DefaultEnv: map[string]string{
			"TEST_ENV": "test_value",
		},
		DefaultWorkDir: "/tmp",
	}

	executor := NewNativeExecutor(logger, config)
	assert.Equal(t, config.DefaultEnv, executor.defaultEnv)
	assert.Equal(t, config.DefaultWorkDir, executor.defaultWD)

	// Test that environment variables are merged properly
	process, err := executor.NewProcess("echo $TEST_ENV", exec.ProcessOptions{
		Env: map[string]string{
			"ANOTHER_ENV": "another_value",
		},
	})
	assert.NoError(t, err)

	// Start process
	err = process.Start()
	assert.NoError(t, err)

	// Read output
	sb := new(strings.Builder)
	for {
		buf := make([]byte, 65536)
		_, err = process.Stdout().Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, fs.ErrClosed) {
				break
			}
			t.Fatal(err)
		}
		sb.Write(buf)
	}

	// Output should contain the environment variable value
	assert.Contains(t, sb.String(), "test_value")
}
