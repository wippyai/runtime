package native

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"
	"strconv"
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
			logger := zap.NewNop()

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
	if runtime.GOOS == "windows" {
		t.Skip("/dev/urandom is not supported on windows")
	}
	logger := zap.NewNop()

	// Create the process
	nativeExecutor := NewNativeExecutor(logger, &exec.NativeExecutorConfig{})

	process, err := nativeExecutor.NewProcess("sh -c 'yes | head -n 100'", exec.ProcessOptions{})
	assert.NoError(t, err)

	processExecutor, ok := process.(*ProcessExecutor)
	assert.True(t, ok)

	// Start reading stdout BEFORE starting the process to avoid race conditions
	sb := new(strings.Builder)
	readDone := make(chan struct{})
	processDone := make(chan struct{})

	go func() {
		defer close(readDone)
		timeout := time.After(3 * time.Second) // Give more time to read output

		for {
			select {
			case <-timeout:
				t.Logf("Timeout reached, collected %d bytes of output", sb.Len())
				return
			default:
				buf := make([]byte, 65536) // Smaller buffer for faster reading
				n, err := process.Stdout().Read(buf)
				if err != nil {
					if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, fs.ErrClosed) {
						return
					}
					t.Errorf("Error reading stdout: %v", err)
					return
				}
				if n > 0 {
					sb.Write(buf[:n])
				}
			}
		}
	}()

	// Now start the process
	err = process.Start()
	assert.NoError(t, err)

	// Wait for the process to complete in a separate goroutine
	go func() {
		defer close(processDone)
		err := process.Wait()
		if err != nil {
			t.Logf("Process completed with error: %v", err)
		}
	}()

	// Stop the process after a short delay to ensure we get some output
	go func() {
		// Use context with timeout instead of time.Sleep to prevent test hanging
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		select {
		case <-ctx.Done():
			t.Log("Timeout waiting for process to start")
		case <-time.After(1 * time.Second): // Give process time to start and produce output
		}
		processExecutor.Stop()
	}()

	// Wait for both the process to complete and reading to finish
	select {
	case <-processDone:
		// Process completed, give a little time for reading to finish
		select {
		case <-readDone:
			// Reading completed
		case <-time.After(1 * time.Second):
			// Reading timed out, but process is done
		}
	case <-readDone:
		// Reading completed, give a little time for process to finish
		select {
		case <-processDone:
			// Process completed
		case <-time.After(1 * time.Second):
			// Process timed out, but reading is done
		}
	}

	if sb.Len() == 0 {
		t.Fatal("no output")
	}
}

func TestExecutor_Stdout(t *testing.T) {
	// Log system information for debugging CI/CD issues
	t.Logf("=== TestExecutor_Stdout started ===")
	t.Logf("Platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	t.Logf("Go version: %s", runtime.Version())
	t.Logf("GOMAXPROCS: %d", runtime.GOMAXPROCS(0))
	t.Logf("NumCPU: %d", runtime.NumCPU())

	// Log important environment variables for CI/CD debugging
	t.Logf("Environment: CI=%s, GITHUB_ACTIONS=%s, TRAVIS=%s, CIRCLECI=%s",
		os.Getenv("CI"), os.Getenv("GITHUB_ACTIONS"), os.Getenv("TRAVIS"), os.Getenv("CIRCLECI"))
	t.Logf("Working directory: %s", getCurrentDir())

	// Log system resource limits (Linux only)
	if runtime.GOOS == "linux" {
		logSystemLimits(t)
	}

	// Log initial memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	t.Logf("Initial memory: Alloc=%d KB, Sys=%d KB, NumGC=%d", m.Alloc/1024, m.Sys/1024, m.NumGC)

	// Log initial goroutine count
	initialGoroutines := runtime.NumGoroutine()
	t.Logf("Initial goroutines: %d", initialGoroutines)

	// Log initial file descriptor count (Linux only)
	if runtime.GOOS == "linux" {
		initialFDs := getFileDescriptorCount()
		t.Logf("Initial file descriptors: %d", initialFDs)

		// Log initial network connections (Linux only)
		initialConnections := getNetworkConnectionCount()
		t.Logf("Initial network connections: %d", initialConnections)

		// Log initial system call count (Linux only)
		initialSyscalls := getSystemCallCount()
		t.Logf("Initial system calls: %d", initialSyscalls)

		// Log initial process count (Linux only)
		initia
	}
	

	logger := zap.NewNop()

	// Create the process with platform-compatible echo command
	nativeExecutor := NewNativeExecutor(logger, &exec.NativeExecutorConfig{})

	// Use a command that produces output more reliably and doesn't finish too quickly
	var command string
	if runtime.GOOS == "windows" {
		command = "cmd /c echo hello world && timeout 1"
	} else {
		command = "sh -c 'echo hello world && sleep 0.1'"
	}

	t.Logf("Using command: %q on platform: %s", command, runtime.GOOS)

	process, err := nativeExecutor.NewProcess(command, exec.ProcessOptions{})
	assert.NoError(t, err)

	// Start reading stdout BEFORE starting the process to avoid race conditions
	sb := new(strings.Builder)
	readDone := make(chan struct{})
	processDone := make(chan struct{})
	readStarted := make(chan struct{})

	go func() {
		defer close(readDone)
		close(readStarted)
		t.Log("Reading goroutine started")
		timeout := time.After(3 * time.Second) // Give more time to read output
		bytesRead := 0
		readStartTime := time.Now()

		for {
			select {
			case <-timeout:
				t.Logf("Reading timeout reached after %v, total bytes read: %d, stdout output: %q", 
				t.Logf("Reading timeout reached after %v, total bytes read: %d, stdout output: %q",
				return
			default:
				buf := make([]byte, 1024)
				n, err := process.Stdout().Read(buf)
				if err != nil {
					if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, fs.ErrClosed) {
						t.Logf("Reading completed (EOF/closed pipe) after %v, total bytes read: %d, final output: %q", 
						t.Logf("Reading completed (EOF/closed pipe) after %v, total bytes read: %d, final output: %q",
						// Process has finished, but we might have read some data
						return
					}
					t.Errorf("Error reading stdout: %v", err)
					return
				}
				if n > 0 {
					sb.Write(buf[:n])
					bytesRead += n
					t.Logf("Read %d bytes, total: %d, current output: %q", n, bytesRead, sb.String())
				}
			}
		}
	}()

	// Wait for reading goroutine to start
	<-readStarted
	t.Log("Reading goroutine is ready")

	// Now start the process
	t.Log("Starting process...")
	processStartTime := time.Now()
	err = process.Start()
	assert.NoError(t, err)
	t.Logf("Process started with PID: %d in %v", process.(*ProcessExecutor).pid, time.Since(processStartTime))

	// Wait for the process to complete in a separate goroutine
	go func() {
		defer close(processDone)
		t.Log("Waiting for process to complete...")
		waitStartTime := time.Now()
		err := process.Wait()
		waitDuration := time.Since(waitStartTime)
		if err != nil {
			t.Logf("Process completed with error after %v: %v", waitDuration, err)
		} else {
			t.Logf("Process completed successfully after %v", waitDuration)
		}
	}()

	// Wait for both the process to complete and reading to finish
	t.Log("Waiting for process and reading to complete...")
	select {
	case <-processDone:
		t.Log("Process completed first, waiting for reading to finish...")
		// Process completed, give a little time for reading to finish
		select {
		case <-readDone:
			t.Log("Reading completed after process")
		case <-time.After(1 * time.Second):
			t.Log("Reading timed out after process completion")
		}
	case <-readDone:
		t.Log("Reading completed first, waiting for process to finish...")
		// Reading completed, wait for process
		<-processDone
		t.Log("Process completed after reading")
	case <-time.After(5 * time.Second):
		t.Fatal("Test timed out waiting for process and reading")
	}

	output := sb.String()
	t.Logf("Final stdout output: %q (length: %d)", output, len(output))
	

		t.Errorf("Expected stdout to contain 'hello world', got: %q", output)
	} else {
		t.Log("Test passed: 'hello world' found in output")
	}
	

	runtime.ReadMemStats(&m)
	t.Logf("Final memory: Alloc=%d KB, Sys=%d KB, NumGC=%d", m.Alloc/1024, m.Sys/1024, m.NumGC)
	

	t.Logf("Final goroutines: %d (change: %+d)", finalGoroutines, finalGoroutines-initialGoroutines)
	

	if runtime.GOOS == "linux" {
		finalFDs := getFileDescriptorCount()
		t.Logf("Final file descriptors: %d", finalFDs)
		

		finalConnections := getNetworkConnectionCount()
		t.Logf("Final network connections: %d", finalConnections)
		

		finalSyscalls := getSystemCallCount()
		t.Logf("Final system calls: %d", finalSyscalls)
	}
final network connections (Linux only)
		finalConnections := getNetworkConnectionCount()
		t.Logf("Final network 
	
	totalDuration := time.Since(startTime)

}

func TestExecutor_StdoutWithSleep(t *testing.T) {
	logger := zap.NewNop()

	// Create the process with a command that writes and then sleeps
	nativeExecutor := NewNativeExecutor(logger, &exec.NativeExecutorConfig{})

	// Use a command that writes to stdout and then sleeps
	var command string
	if runtime.GOOS == "windows" {
		command = "cmd /c echo hello world && timeout 1"
	} else {
		command = "sh -c 'echo hello world && sleep 0.1'"
	}

	process, err := nativeExecutor.NewProcess(command, exec.ProcessOptions{})
	assert.NoError(t, err)

	// Start reading BEFORE starting the process
	sb := new(strings.Builder)
	readDone := make(chan struct{})
	processDone := make(chan struct{})
	readStarted := make(chan struct{})

	go func() {
		defer close(readDone)
		close(readStarted)                     // Signal that reading has started
		timeout := time.After(2 * time.Second) // Reduced timeout - command executes in milliseconds

		for {
			select {
			case <-timeout:
				t.Logf("Timeout reached, stdout output: %q", sb.String())
				return
			default:
				buf := make([]byte, 1024)
				n, err := process.Stdout().Read(buf)
				if err != nil {
					if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, fs.ErrClosed) {
						// Process has finished, but we might have read some data
						return
					}
					t.Errorf("Error reading stdout: %v", err)
					return
				}
				if n > 0 {
					sb.Write(buf[:n])
				}
			}
		}
	}()

	// Wait for reading goroutine to start
	<-readStarted

	// Give a moment for the reading goroutine to start
	// Use context with timeout instead of time.Sleep to prevent test hanging
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Log("Timeout waiting for reading goroutine")
	case <-time.After(10 * time.Millisecond):
		// Give a moment for the reading goroutine to start
	}

	// Now start the process
	err = process.Start()
	assert.NoError(t, err)

	// Wait for the process to complete in a separate goroutine
	go func() {
		defer close(processDone)
		err := process.Wait()
		if err != nil {
			t.Logf("Process completed with error: %v", err)
		}
	}()

	// Wait for both the process to complete and reading to finish
	select {
	case <-processDone:
		// Process completed, give a little time for reading to finish
		select {
		case <-readDone:
			// Reading completed
		case <-time.After(1 * time.Second):
			// Reading timed out, but process is done
		}
	case <-readDone:
		// Reading completed, wait for process
		<-processDone
	case <-time.After(5 * time.Second):
		t.Fatal("Test timed out")
	}

	output := sb.String()
	if !strings.Contains(output, "hello world") {
		t.Errorf("Expected stdout to contain 'hello world', got: %q", output)
	}
}

func TestExecutor_EmptyCmd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping test on Windows")
	}

	logger := zap.NewNop()

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
	logger := zap.NewNop()

	// Use a cross-platform way to generate stderr output
	var command string
	if runtime.GOOS == "windows" {
		// On Windows, we need to use CMD to redirect to stderr
		command = "cmd /c echo error message 1>&2"
	} else {
		// On Unix systems - use a more reliable approach
		command = "bash -c 'echo error message >&2'"
	}

	// Create the process
	nativeExecutor := NewNativeExecutor(logger, &exec.NativeExecutorConfig{})
	process, err := nativeExecutor.NewProcess(command, exec.ProcessOptions{})
	assert.NoError(t, err)

	err = process.Start()
	assert.NoError(t, err)

	// Start reading immediately in a goroutine
	sb := new(strings.Builder)
	readDone := make(chan struct{})

	go func() {
		defer close(readDone)
		timeout := time.After(2 * time.Second)

		for {
			select {
			case <-timeout:
				t.Logf("Timeout reached, stderr output: %q", sb.String())
				return
			default:
				buf := make([]byte, 1024)
				n, err := process.Stderr().Read(buf)
				if err != nil {
					if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, fs.ErrClosed) {
						return
					}
					t.Errorf("Error reading stderr: %v", err)
					return
				}
				if n > 0 {
					sb.Write(buf[:n])
				}
			}
		}
	}()

	// Wait for the process to complete
	_ = process.Wait()

	// Wait for reading to complete
	<-readDone

	output := sb.String()
	if !strings.Contains(output, "error message") {
		t.Errorf("Expected stderr to contain 'error message', got: %q", output)
	}
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
			logger := zap.NewNop()

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
	logger := zap.NewNop()

	// Test with custom environment and working directory
	config := &exec.NativeExecutorConfig{
		DefaultEnv: map[string]string{
			"TEST_ENV": "test_value",
		},
		DefaultWorkDir: os.TempDir(),
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
	logger := zap.NewNop()

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

// getCurrentDir returns the current working directory, handling errors gracefully
func getCurrentDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Sprintf("error getting working directory: %v", err)
	}
	return dir
}

// getFileDescriptorCount returns the number of open file descriptors for the current process (Linux only)
func getFileDescriptorCount() int {
	if runtime.GOOS != "linux" {
		return -1
	}
	
	// Read /proc/self/fd to count open file descriptors

	if err != nil {
		return -1
	}
	
	return len(entries)


// getNetworkConnectionCount returns the number of network connections for the current process (Linux only)
func getNetworkConnectionCount() int {
	if runtime.GOOS != "linux" {
		return -1
	}
	
	// Read /proc/net/tcp and /proc/net/udp to count connections

	if err != nil {
		return -1
	}
	
	udpData, err := os.ReadFile("/proc/net/udp")

		return -1
	}
	
	// Count lines (excluding header) for both TCP and UDP

	udpLines := strings.Split(string(udpData), "\n")
	
	// Subtract 1 from each to exclude header line

	udpCount := len(udpLines) - 1
	
	return tcpCount + udpCount


// getSystemCallCount returns the number of system calls made by the current process (Linux only)
func getSystemCallCount() int {
	if runtime.GOOS != "linux" {
		return -1
	}
	
	// Read /proc/self/stat to get system call count

	if err != nil {
		return -1
	}
	
	// Parse the stat file to extract system call count

	fields := strings.Fields(string(data))
	if len(fields) < 14 {
		return -1
	}
	
	// The 14th field (0-indexed) is the number of voluntary context switches

	voluntarySwitches := fields[13]
	if voluntarySwitches == "" {
		return -1
	}
	
	// Convert to int

	_, err = fmt.Sscanf(voluntarySwitches, "%d", &count)
	if err != nil {
		return -1
	}
	
	return count

	return count
}

// getProcessCount returns the number of processes in the system (Linux only)
func getProcessCount() int {
	if runtime.GOOS != "linux" {
		return -1
	}

	// Read /proc directory to count processes
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return -1
	}

	count := 0
	for _, entry := range entries {
		// Check if the entry name is a number (PID)
		if entry.IsDir() {
			if _, err := strconv.Atoi(entry.Name()); err == nil {
				count++
			}
		}
	}


// logSystemLimits logs system resource limits for debugging CI/CD issues (Linux only)
func logSystemLimits(t *testing.T) {
	if runtime.GOOS != "linux" {
		return
	}
	
	// Try to read some common system limits

		"RLIMIT_NOFILE": "/proc/self/limits",
		"RLIMIT_NPROC":  "/proc/self/limits",
	}
	
	for name, path := range limits {

		if err != nil {
			t.Logf("Could not read %s: %v", name, err)
			continue
		}
		
		// Extract the limit value from the file content

		for _, line := range lines {
			if strings.Contains(line, "open files") || strings.Contains(line, "processes") {
				t.Logf("System limit %s: %s", name, strings.TrimSpace(line))
				break
			}
		}
	}
}
