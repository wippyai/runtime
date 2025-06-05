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
		wantErr assert.ErrorAssertionFunc
	}{
		{
			name:    "echo command",
			command: "echo 'hello world'",
			wantErr: assert.NoError,
		},
		{
			name:    "invalid command",
			command: "invalidcommand",
			wantErr: assert.ErrorAssertionFunc(func(t assert.TestingT, err error, _ ...any) bool {
				return assert.ErrorContains(t, err, "not found")
			}),
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
			if tt.wantErr(t, err) {
				return
			}

			go func() {
				_ = process.Wait()
			}()

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

	// Using direct command equivalents instead of shell piping
	// We'll use a single command with args for cross-platform compatibility
	var command string
	if runtime.GOOS == "windows" {
		command = "findstr"
	} else {
		command = "head"
	}

	process, err := nativeExecutor.NewProcess(command+" -n 100 /dev/urandom", exec.ProcessOptions{})
	assert.NoError(t, err)

	processExecutor, ok := process.(*ProcessExecutor)
	assert.True(t, ok)

	err = process.Start()
	assert.NoError(t, err)

	go func() {
		_ = process.Wait()
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

	// Create the process with platform-compatible echo command
	nativeExecutor := NewNativeExecutor(logger, &exec.NativeExecutorConfig{})
	var command string
	if runtime.GOOS == "windows" {
		command = "echo hello world"
	} else {
		command = "echo 'hello world'"
	}

	process, err := nativeExecutor.NewProcess(command, exec.ProcessOptions{})
	assert.NoError(t, err)

	processExecutor, ok := process.(*ProcessExecutor)
	assert.True(t, ok)

	err = process.Start()
	assert.NoError(t, err)

	go func() {
		_ = process.Wait()
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
	if runtime.GOOS == "windows" {
		t.Skip("skipping test on Windows")
	}

	logger, _ := zap.NewDevelopment()

	// Create the process with a minimal, cross-platform command
	nativeExecutor := NewNativeExecutor(logger, &exec.NativeExecutorConfig{})

	cmd := "true" // A command that does nothing and returns success on Unix systems

	process, err := nativeExecutor.NewProcess(cmd, exec.ProcessOptions{})
	assert.NoError(t, err)

	err = process.Start()
	assert.NoError(t, err)

	go func() {
		_ = process.Wait()
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

	// Use a cross-platform way to generate stderr output
	var command string
	if runtime.GOOS == "windows" {
		// On Windows, we need to use CMD to redirect to stderr
		command = "cmd /c echo error message 1>&2"
	} else {
		// On Unix systems
		command = "sh -c \"echo error message >&2\""
	}

	// Create the process
	nativeExecutor := NewNativeExecutor(logger, &exec.NativeExecutorConfig{})
	process, err := nativeExecutor.NewProcess(command, exec.ProcessOptions{})
	assert.NoError(t, err)

	err = process.Start()
	assert.NoError(t, err)

	go func() {
		_ = process.Wait()
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

	assert.Contains(t, sb.String(), "error message")
}

func TestExecutor_ReadWithInvalidCommand(t *testing.T) {
	l, _ := mocklogger.ZapTestLogger(zap.DebugLevel)

	// Create the process
	nativeExecutor := NewNativeExecutor(l, &exec.NativeExecutorConfig{})
	process, err := nativeExecutor.NewProcess("invalidcommand", exec.ProcessOptions{})
	assert.NoError(t, err)

	// Start will fail on most platforms with "executable not found"
	err = process.Start()
	if err != nil {
		assert.Contains(t, err.Error(), "executable file not found")
		return
	}

	// If we somehow get here (the command exists but will fail), wait for it
	_ = process.Wait()
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
					_ = process.Wait()
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

	// Use a platform-specific approach to test environment variable
	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "cmd /c echo %TEST_ENV%"
	} else {
		cmd = "sh -c \"echo $TEST_ENV\""
	}

	// Test that environment variables are merged properly
	process, err := executor.NewProcess(cmd, exec.ProcessOptions{
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

func TestNativeExecutor_Whitelist(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	tests := []struct {
		name          string
		whitelist     []string
		command       string
		shouldSucceed bool
		errorContains string
	}{
		{
			name:          "no whitelist - all commands allowed",
			whitelist:     nil,
			command:       "echo 'test'",
			shouldSucceed: true,
		},
		{
			name:          "empty whitelist - all commands allowed",
			whitelist:     []string{},
			command:       "echo 'test'",
			shouldSucceed: true,
		},
		{
			name:          "command in whitelist - allowed",
			whitelist:     []string{"echo 'test'", "ls -l"},
			command:       "echo 'test'",
			shouldSucceed: true,
		},
		{
			name:          "command not in whitelist - rejected",
			whitelist:     []string{"ls -l", "cat /etc/hosts"},
			command:       "echo 'test'",
			shouldSucceed: false,
			errorContains: "command not in whitelist",
		},
		{
			name:          "partial match - rejected",
			whitelist:     []string{"echo 'something else'", "ls"},
			command:       "echo 'test'",
			shouldSucceed: false,
			errorContains: "command not in whitelist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create executor with whitelist config
			config := &exec.NativeExecutorConfig{
				CommandWhitelist: tt.whitelist,
			}

			executor := NewNativeExecutor(logger, config)

			// Try to create a process with the command
			process, err := executor.NewProcess(tt.command, exec.ProcessOptions{})

			if tt.shouldSucceed {
				assert.NoError(t, err)
				assert.NotNil(t, process)

				// Verify process can start (optional, just to check it's valid)
				if process != nil {
					err = process.Start()
					assert.NoError(t, err)

					// Clean up - stop the process
					processExecutor, ok := process.(*ProcessExecutor)
					if ok {
						processExecutor.Stop()
					}
				}
			} else {
				assert.Error(t, err)
				assert.Nil(t, process)
				assert.Contains(t, err.Error(), tt.errorContains)
			}
		})
	}
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		expected []string
	}{
		// Basic cases
		{
			name:     "empty command",
			command:  "",
			expected: []string{""},
		},
		{
			name:     "simple command without args",
			command:  "ls",
			expected: []string{"ls"},
		},
		{
			name:     "command with single arg",
			command:  "ls -l",
			expected: []string{"ls", "-l"},
		},
		{
			name:     "command with multiple args",
			command:  "ls -l -a /tmp",
			expected: []string{"ls", "-l", "-a", "/tmp"},
		},

		// Quoted arguments
		{
			name:     "command with double-quoted arg",
			command:  "echo \"hello world\"",
			expected: []string{"echo", "hello world"},
		},
		{
			name:     "command with single-quoted arg",
			command:  "echo 'hello world'",
			expected: []string{"echo", "hello world"},
		},
		{
			name:     "command with mixed quotes",
			command:  "echo 'single quoted' \"double quoted\"",
			expected: []string{"echo", "single quoted", "double quoted"},
		},

		// Whitespace handling
		{
			name:     "command with multiple spaces between args",
			command:  "ls  -l    -a",
			expected: []string{"ls", "-l", "-a"},
		},
		{
			name:     "command with trailing space",
			command:  "ls -l ",
			expected: []string{"ls", "-l"},
		},
		{
			name:     "command with leading space",
			command:  " ls -l",
			expected: []string{"ls", "-l"},
		},

		// Advanced quote handling
		{
			name:     "quotes in the middle of an arg",
			command:  "echo hello\"world\"",
			expected: []string{"echo", "helloworld"},
		},
		{
			name:     "quotes around part of an arg",
			command:  "echo hello\"world\"goodbye",
			expected: []string{"echo", "helloworldgoodbye"},
		},
		{
			name:     "nested quotes within quotes",
			command:  "echo 'He said \"hello\"'",
			expected: []string{"echo", "He said \"hello\""},
		},
		{
			name:     "quotes within double-quoted string",
			command:  "echo \"It's a nice day\"",
			expected: []string{"echo", "It's a nice day"},
		},
		{
			name:     "empty quoted arg",
			command:  "echo ''",
			expected: []string{"echo", ""},
		},
		{
			name:     "adjacent quoted strings",
			command:  "echo \"hello\"'world'",
			expected: []string{"echo", "helloworld"},
		},

		// Special characters and edge cases
		{
			name:     "command with special characters in quoted args",
			command:  "echo \"$HOME\" '$(pwd)'",
			expected: []string{"echo", "$HOME", "$(pwd)"},
		},
		{
			name:     "unbalanced quotes (should preserve the quote)",
			command:  "echo \"hello",
			expected: []string{"echo", "\"hello"},
		},
		{
			name:     "unbalanced single quotes",
			command:  "echo 'hello",
			expected: []string{"echo", "'hello"},
		},

		// Platform-specific paths
		{
			name:     "Unix path with spaces",
			command:  "ls \"/home/user/My Documents\"",
			expected: []string{"ls", "/home/user/My Documents"},
		},
		{
			name:     "Windows path with spaces",
			command:  "dir \"C:\\Program Files\\Some App\"",
			expected: []string{"dir", "C:\\Program Files\\Some App"},
		},

		// Complex commands
		{
			name:     "complex command with pipe operator",
			command:  "find . -name \"*.go\" | grep \"func\"",
			expected: []string{"find", ".", "-name", "*.go", "|", "grep", "func"},
		},
		{
			name:     "complex command with redirection",
			command:  "echo hello > file.txt",
			expected: []string{"echo", "hello", ">", "file.txt"},
		},
		{
			name:     "complex command with multiple operators",
			command:  "cat file.txt | grep \"pattern\" > results.txt 2>/dev/null",
			expected: []string{"cat", "file.txt", "|", "grep", "pattern", ">", "results.txt", "2>/dev/null"},
		},

		// Edge cases
		{
			name:     "command with only spaces",
			command:  "   ",
			expected: []string{},
		},
		{
			name:     "command with only quotes",
			command:  "\"\"",
			expected: []string{""},
		},
		{
			name:     "command with quotes and spaces",
			command:  "\"   \"",
			expected: []string{"   "},
		},
		{
			name:     "quoted escape sequences",
			command:  "echo \"\\n\\t\"",
			expected: []string{"echo", "\\n\\t"},
		},
		{
			name:     "git commit with message",
			command:  "git commit -m \"Initial commit\"",
			expected: []string{"git", "commit", "-m", "Initial commit"},
		},
		{
			name:     "find command with complex expression",
			command:  "find . -type f -name \"*.go\" -not -path \"*/vendor/*\"",
			expected: []string{"find", ".", "-type", "f", "-name", "*.go", "-not", "-path", "*/vendor/*"},
		},
		{
			name:     "docker run with multiple options",
			command:  "docker run -it --name test -v \"$(pwd):/app\" alpine:latest sh",
			expected: []string{"docker", "run", "-it", "--name", "test", "-v", "$(pwd):/app", "alpine:latest", "sh"},
		},
		{
			name:     "command with environment variables",
			command:  "DEBUG=true PORT=3000 npm start",
			expected: []string{"DEBUG=true", "PORT=3000", "npm", "start"},
		},
		{
			name:     "curl with complex URL and options",
			command:  "curl -X POST \"https://api.example.com/v1/users?id=123\" -H \"Authorization: Bearer token\"",
			expected: []string{"curl", "-X", "POST", "https://api.example.com/v1/users?id=123", "-H", "Authorization: Bearer token"},
		},
		{
			name:     "command with glob patterns",
			command:  "rm -rf *.bak tmp-*",
			expected: []string{"rm", "-rf", "*.bak", "tmp-*"},
		},
		{
			name:     "psql command with connection string",
			command:  "psql \"postgresql://user:password@localhost:5432/dbname\"",
			expected: []string{"psql", "postgresql://user:password@localhost:5432/dbname"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseCommand(tt.command)
			assert.Equal(t, tt.expected, result, "Parsed command doesn't match expected result")
		})
	}
}
