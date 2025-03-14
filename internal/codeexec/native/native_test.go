package native

import (
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"
	"time"

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
			executor := NewNativeExecutor(logger, WithCmd(tt.command))
			err := executor.Start()
			go func() {
				executor.Wait()
			}()

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			executor.Stop()
		})
	}
}

func TestExecutor_MegaCommand(t *testing.T) {
	logger := zap.NewNop()
	executor := NewNativeExecutor(logger, WithCmd("cat /dev/urandom | hexdump -C"))

	err := executor.Start()
	assert.NoError(t, err)

	go func() {
		executor.Wait()
	}()

	go func() {
		time.Sleep(time.Second * 5)
		executor.Stop()
	}()

	sb := new(strings.Builder)
	for {
		// we don't care about the perf here
		buf := make([]byte, 65536)
		_, err = executor.StdoutReader().Read(buf)
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
	executor := NewNativeExecutor(logger, WithCmd("echo 'hello world'"))

	err := executor.Start()
	assert.NoError(t, err)

	go func() {
		executor.Wait()
		assert.Equal(t, "terminated", executor.State())
	}()

	assert.Equal(t, "running", executor.State())

	sb := new(strings.Builder)

	for {
		// we don't care about the perf here
		buf := make([]byte, 65536)
		_, err = executor.StdoutReader().Read(buf)
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
	executor := NewNativeExecutor(logger, WithCmd(""))

	err := executor.Start()
	assert.NoError(t, err)
	go func() {
		executor.Wait()
	}()

	sb := new(strings.Builder)

	for {
		// we don't care about the perf here
		buf := make([]byte, 65536)
		_, err = executor.StderrReader().Read(buf)
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
	executor := NewNativeExecutor(logger, WithCmd("echo 'error message' >&2"))

	err := executor.Start()
	assert.NoError(t, err)
	go func() {
		executor.Wait()
	}()

	sb := new(strings.Builder)

	for {
		// we don't care about the perf here
		buf := make([]byte, 65536)
		_, err = executor.StderrReader().Read(buf)
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
	l, _ := mocklogger.ZapTestLogger(zap.DebugLevel)
	executor := NewNativeExecutor(l, WithCmd("sleep 1 && invalidcommand"))

	err := executor.Start()
	assert.NoError(t, err)

	go func() {
		executor.Wait()
	}()

	// Wait for an error message in stderr
	sb := new(strings.Builder)

	for {
		// we don't care about the perf here
		buf := make([]byte, 65536)
		_, err = executor.StderrReader().Read(buf)
		if err != nil {
			// fs.ErrClosed is returned when the process is stopped (the file is already closed)
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, fs.ErrClosed) {
				break
			}

			t.Fatal(err)
		}

		sb.Write(buf)
	}

	assert.Contains(t, sb.String(), "sh: invalidcommand: command not found")
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
			executor := NewNativeExecutor(logger, WithCmd(tt.command))

			if tt.command == "cat" {
				err := executor.Start()
				assert.NoError(t, err)

				go func() {
					executor.Wait()
				}()

				go func() {
					err2 := executor.WriteStdin([]byte(tt.input))
					assert.NoError(t, err2)
				}()

				sb := new(strings.Builder)

				for {
					// we don't care about the perf here
					buf := make([]byte, 65536)
					_, err = executor.StdoutReader().Read(buf)
					if err != nil {
						// fs.ErrClosed is returned when the process is stopped (the file is already closed)
						if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, fs.ErrClosed) {
							break
						}

						t.Fatal(err)
					}

					sb.Write(buf)
					executor.Stop()
				}

				assert.Contains(t, sb.String(), tt.expect)
			} else {
				// Test writing to a non-running process
				err := executor.WriteStdin([]byte(tt.input))
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "process is not running")
			}
		})
	}
}
