package tests

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const (
	serverURL = "http://localhost:8082"
	wippyDir  = ".wippy"
)

var (
	testTimeout = getTestTimeout()
)

// TestContext holds test-specific state including completion flag
type TestContext struct {
	completed *int64
}

// NewTestContext creates a new test context
func NewTestContext() *TestContext {
	return &TestContext{
		completed: new(int64),
	}
}

// conditionalLogf logs only if this specific test has not completed successfully yet
func (tc *TestContext) conditionalLogf(t *testing.T, format string, args ...interface{}) {
	t.Helper()
	if atomic.LoadInt64(tc.completed) == 0 {
		t.Logf(format, args...)
	}
}

// markTestCompleted sets the flag indicating this specific test completed successfully
func (tc *TestContext) markTestCompleted() {
	atomic.StoreInt64(tc.completed, 1)
}

// getTestTimeout returns appropriate timeout based on environment
func getTestTimeout() time.Duration {
	// Check for CI environment
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		return 45 * time.Second // Increased timeout for CI - server startup can take 35-40s
	}
	return 10 * time.Second // Fast timeout for local development
}

// ExpectedModule represents expected module structure
type ExpectedModule struct {
	Name string
	Path string
}

var expectedModules = []ExpectedModule{
	{Name: "wippy/llm", Path: "wippy/llm@0198804f-dfb2-7197-b156-98315cb39ed5"},
	{Name: "wippy/security", Path: "wippy/security@01978c92-7d02-7b4a-95df-55b57cfe80b7"},
	{Name: "wippy/test", Path: "wippy/test@0197e530-927f-75f5-995c-b6f5e0dd32f9"},
}

// getProjectRoot dynamically determines the project root directory
func getProjectRoot(t *testing.T) string {
	t.Helper()

	// Start from current working directory
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Look for go.mod file to identify project root
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root without finding go.mod
			break
		}
		dir = parent
	}

	// Fallback: assume the parent directory of tests/ is the project root
	if strings.HasSuffix(wd, "tests") {
		return filepath.Dir(wd)
	}

	// Another fallback: assume current directory is project root
	return wd
}

// runCommand executes a command and redirects stderr to logFile (like shell 2> redirection)
func runCommand(t *testing.T, command string, args []string, logFile string) (cmd *exec.Cmd, logFileHandle *os.File, err error) {
	t.Helper()

	projectRoot := getProjectRoot(t)

	// Change to project root directory
	if err := os.Chdir(projectRoot); err != nil {
		return nil, nil, fmt.Errorf("failed to change directory: %w", err)
	}

	cmd = exec.Command(command, args...)
	cmd.Dir = projectRoot

	// Create log file for stderr and redirect all stderr to it (like 2> redirection)
	logFileHandle, err = os.Create(logFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create log file %s: %w", logFile, err)
	}

	// Redirect stderr directly to file
	cmd.Stderr = logFileHandle

	return cmd, logFileHandle, nil
}

// LogAnalyzer performs real-time log analysis for immediate failure detection
type LogAnalyzer struct {
	failurePatterns []string
	successPatterns []string
}

// NewLogAnalyzer creates a new log analyzer with common failure and success patterns
func NewLogAnalyzer() *LogAnalyzer {
	return &LogAnalyzer{
		failurePatterns: []string{
			"panic:",
			"fatal error:",
			"failed to start",
			"error loading",
			"connection refused",
			"bind: address already in use",
			"port already in use",
			"listen tcp",
			"failed to bind",
			"startup error",
			"initialization failed",
			"module load error",
			"dependency error",
			"configuration error",
			"database connection failed",
			"redis connection failed",
		},
		successPatterns: []string{
			"application started successfully",
			"server started",
			"service started",
			"ready to serve",
			"listening on",
			"server is ready",
		},
	}
}

// AnalyzeLine analyzes a log line and returns analysis result
func (la *LogAnalyzer) AnalyzeLine(line string) (isFailure bool, isSuccess bool, message string) {
	lowerLine := strings.ToLower(line)

	// Check for failure patterns first (higher priority)
	for _, pattern := range la.failurePatterns {
		if strings.Contains(lowerLine, pattern) {
			return true, false, fmt.Sprintf("Detected failure pattern '%s' in: %s", pattern, line)
		}
	}

	// Check for success patterns
	for _, pattern := range la.successPatterns {
		if strings.Contains(lowerLine, pattern) {
			return false, true, fmt.Sprintf("Detected success pattern '%s'", pattern)
		}
	}

	return false, false, ""
}

// waitForServerStart waits for the server to start by checking logs and HTTP endpoint
//
//nolint:unused // keeping for potential future use
func waitForServerStart(ctx context.Context, t *testing.T, stderr io.Reader) error {
	t.Helper()

	scanner := bufio.NewScanner(stderr)
	timeout := time.After(testTimeout)
	serverReady := make(chan bool, 1)
	logOutput := make(chan string, 5000) // Buffered channel for log lines
	scannerDone := make(chan bool, 1)
	logAnalyzer := NewLogAnalyzer()
	var collectedLogs []string // Collect all logs for timeout debugging

	t.Logf("Waiting for server to start (timeout: %v) with real-time log analysis", testTimeout)

	// Read logs in separate goroutine with immediate analysis
	go func() {
		defer close(scannerDone)
		for scanner.Scan() {
			line := scanner.Text()
			select {
			case logOutput <- line:
			case <-timeout:
				return
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			// Only log if context is not canceled (avoids cleanup error noise)
			select {
			case <-ctx.Done():
				// Context canceled, don't log cleanup errors
			default:
				t.Logf("Error reading stderr: %v", err)
			}
		}
	}()

	// Check server readiness via HTTP in parallel (more frequent checks)
	go func() {
		httpCheckInterval := time.NewTicker(200 * time.Millisecond) // More frequent checks
		defer httpCheckInterval.Stop()

		for {
			select {
			case <-timeout:
				return
			case <-ctx.Done():
				return
			case <-httpCheckInterval.C:
				reqCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second) // Shorter timeout
				req, err := http.NewRequestWithContext(reqCtx, "GET", serverURL, nil)
				if err != nil {
					cancel()
					continue
				}
				resp, err := http.DefaultClient.Do(req)
				cancel()
				if err == nil {
					resp.Body.Close()
					select {
					case serverReady <- true:
					default:
					}
					return
				}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled while waiting for server to start")
		case <-timeout:
			logOutput := ""
			if len(collectedLogs) > 0 {
				logOutput = "\n=== SERVER LOGS ===\n" + strings.Join(collectedLogs, "\n") + "\n=== END LOGS ===\n"
			}
			return fmt.Errorf("timeout waiting for server to start - no critical errors detected but server didn't become ready%s", logOutput)
		case <-serverReady:
			t.Log("Server is ready and responding to HTTP requests")
			// Shorter wait time since we have real-time analysis
			time.Sleep(1 * time.Second)
			return nil
		case line := <-logOutput:
			t.Logf("Server log: %s", line)
			collectedLogs = append(collectedLogs, line) // Collect logs for potential timeout debugging

			// Real-time log analysis for immediate failure detection
			isFailure, isSuccess, message := logAnalyzer.AnalyzeLine(line)
			if isFailure {
				return fmt.Errorf("server startup failed: %s", message)
			}
			if isSuccess {
				t.Logf("Success indicator detected: %s", message)
			}

		case <-scannerDone:
			t.Log("Log scanner finished - checking if server is responsive")
			// Scanner is done, give HTTP check a bit more time
			select {
			case <-serverReady:
				t.Log("Server is ready and responding to HTTP requests")
				time.Sleep(1 * time.Second)
				return nil
			case <-time.After(3 * time.Second): // Shorter wait
				logOutput := ""
				if len(collectedLogs) > 0 {
					logOutput = "\n=== SERVER LOGS ===\n" + strings.Join(collectedLogs, "\n") + "\n=== END LOGS ===\n"
				}
				return fmt.Errorf("server logs ended but server is not responding to HTTP requests%s", logOutput)
			case <-ctx.Done():
				return fmt.Errorf("context canceled while waiting for server response")
			}
		}
	}
}

// ProcessMonitor manages a single cmd.Wait() call to avoid race conditions
type ProcessMonitor struct {
	cmd     *exec.Cmd
	done    chan error
	started bool
	mu      sync.Mutex
}

// NewProcessMonitor creates a new process monitor for the given command
func NewProcessMonitor(cmd *exec.Cmd) *ProcessMonitor {
	return &ProcessMonitor{
		cmd:  cmd,
		done: make(chan error, 1),
	}
}

// Start begins monitoring the process (can only be called once)
func (pm *ProcessMonitor) Start() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.started {
		return // Already started
	}
	pm.started = true

	go func() {
		err := pm.cmd.Wait()
		pm.done <- err
	}()
}

// Done returns a channel that will receive the process exit error
func (pm *ProcessMonitor) Done() <-chan error {
	return pm.done
}

// waitForServerStartWithLogFile waits for server start while monitoring process health, reading from log file
func waitForServerStartWithLogFile(t *testing.T, cmd *exec.Cmd, logFile string) *ProcessMonitor {
	t.Helper()

	// Create and start process monitor
	procMon := NewProcessMonitor(cmd)
	procMon.Start()

	// Create context with timeout for proper cancellation
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout+10*time.Second)
	defer cancel()

	// Start server waiting in background with context
	waitErr := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				waitErr <- fmt.Errorf("panic in waitForServerStartFromFile: %v", r)
			}
		}()

		// Use context-aware waitForServerStartFromFile
		err := waitForServerStartFromFile(ctx, t, logFile)
		waitErr <- err
	}()

	// Wait for either server ready, process exit, or timeout
	select {
	case err := <-procMon.Done():
		cancel() // Cancel waiting goroutines
		if err != nil {
			t.Fatalf("Server process exited with error before starting: %v", err)
		} else {
			t.Fatal("Server process exited successfully before server was ready")
		}
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("Error waiting for server: %v", err)
		}
		t.Log("Server started successfully")
		return procMon
	case <-ctx.Done():
		// Context timeout - this shouldn't happen since waitForServerStartFromFile should return first
		t.Fatal("Context timeout exceeded - killing process")
	}
	return nil
}

// waitForServerStartWithProcessMonitoring waits for server start while monitoring process health

// waitForServerStartFromFile waits for server start by tailing the log file
func waitForServerStartFromFile(ctx context.Context, t *testing.T, logFile string) error {
	t.Helper()

	timeout := time.After(testTimeout)
	serverReady := make(chan bool, 1)
	scannerDone := make(chan bool, 1)
	logOutput := make(chan string, 100)

	analyzer := NewLogAnalyzer()
	var collectedLogs []string

	t.Logf("Waiting for server to start (timeout: %v) by tailing log file: %s", testTimeout, logFile)

	// Tail the log file
	go func() {
		defer close(scannerDone)

		var file *os.File
		var scanner *bufio.Scanner
		var lastSize int64

		ticker := time.NewTicker(100 * time.Millisecond) // Check file every 100ms
		defer ticker.Stop()
		defer func() {
			if file != nil {
				file.Close()
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Check if file exists and has new content
				info, err := os.Stat(logFile)
				if err != nil {
					continue // File doesn't exist yet, keep waiting
				}

				if info.Size() == lastSize {
					continue // No new content
				}

				// Open file for reading if not already open, or reopen if it was recreated
				if file == nil {
					file, err = os.Open(logFile)
					if err != nil {
						continue
					}
					scanner = bufio.NewScanner(file)
					// Seek to the last position
					if lastSize > 0 {
						if _, err := file.Seek(lastSize, 0); err != nil {
							// If seek fails, continue from beginning
							t.Logf("Failed to seek to position %d in log file: %v", lastSize, err)
						}
					}
				}

				// Read new lines
				for scanner.Scan() {
					line := scanner.Text()
					select {
					case logOutput <- line:
					case <-ctx.Done():
						return
					}
				}

				// Check for scanner errors
				if err := scanner.Err(); err != nil {
					// File might have been rotated or closed, reopen on next iteration
					file.Close()
					file = nil
					scanner = nil
					continue
				}

				lastSize = info.Size()
			}
		}
	}()

	// Check server readiness via HTTP in parallel
	go func() {
		httpCheckInterval := time.NewTicker(200 * time.Millisecond)
		defer httpCheckInterval.Stop()

		client := &http.Client{Timeout: 1 * time.Second}

		for {
			select {
			case <-httpCheckInterval.C:
				req, err := http.NewRequestWithContext(ctx, "GET", serverURL, nil)
				if err != nil {
					continue
				}
				if resp, err := client.Do(req); err == nil {
					resp.Body.Close()
					if resp.StatusCode < 500 {
						select {
						case serverReady <- true:
						default:
						}
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Main wait loop
	for {
		select {
		case <-timeout:
			logOutput := ""
			if len(collectedLogs) > 0 {
				logOutput = "\n=== SERVER LOGS ===\n" + strings.Join(collectedLogs, "\n") + "\n=== END LOGS ===\n"
			}
			return fmt.Errorf("timeout waiting for server to start after %v%s", testTimeout, logOutput)
		case <-serverReady:
			t.Log("Server is ready and responding to HTTP requests")
			time.Sleep(1 * time.Second)
			return nil
		case line := <-logOutput:
			t.Logf("Server log: %s", line)
			collectedLogs = append(collectedLogs, line)

			// Analyze log for immediate failure detection
			isFailure, isSuccess, message := analyzer.AnalyzeLine(line)
			if isFailure {
				return fmt.Errorf("server startup failed: %s", message)
			}
			if isSuccess {
				t.Logf("Success indicator detected: %s", message)
			}

		case <-scannerDone:
			t.Log("Log file scanning finished - checking if server is responsive")
			select {
			case <-serverReady:
				t.Log("Server is ready and responding to HTTP requests")
				time.Sleep(1 * time.Second)
				return nil
			case <-time.After(3 * time.Second):
				logOutput := ""
				if len(collectedLogs) > 0 {
					logOutput = "\n=== SERVER LOGS ===\n" + strings.Join(collectedLogs, "\n") + "\n=== END LOGS ===\n"
				}
				return fmt.Errorf("log file ended but server is not responding to HTTP requests%s", logOutput)
			case <-ctx.Done():
				return fmt.Errorf("context canceled while waiting for server response")
			}
		}
	}
}

// checkModulesExist verifies that expected modules exist in .wippy directory
func checkModulesExist(t *testing.T) {
	t.Helper()

	projectRoot := getProjectRoot(t)
	wippyPath := filepath.Join(projectRoot, wippyDir)

	// Check if .wippy directory exists with retries
	var wippyExists bool
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(wippyPath); err == nil {
			wippyExists = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !wippyExists {
		t.Fatal(".wippy directory does not exist")
	}

	// Check each expected module with retries
	for _, module := range expectedModules {
		modulePath := filepath.Join(wippyPath, module.Path)
		var moduleExists bool
		for i := 0; i < 10; i++ {
			if _, err := os.Stat(modulePath); err == nil {
				moduleExists = true
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if !moduleExists {
			t.Fatalf("Module %s does not exist at path %s", module.Name, modulePath)
		}
		t.Logf("Module %s found at %s", module.Name, modulePath)
	}
}

// checkAPIEndpoint makes a request to the specified endpoint
func checkAPIEndpoint(t *testing.T, endpoint string) {
	t.Helper()

	// Validate and construct URL safely
	baseURL, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("Invalid server URL: %v", err)
	}

	endpointURL, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("Invalid endpoint: %v", err)
	}

	fullURL := baseURL.ResolveReference(endpointURL).String()

	// Create request with context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		t.Fatalf("Failed to create request to %s: %v", fullURL, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request to %s: %v", fullURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Request to %s returned status %d", fullURL, resp.StatusCode)
	}

	// For HTML endpoints, check that we got some content
	if endpoint == "/" {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}
		if len(body) == 0 {
			t.Fatal("Empty response body")
		}
		if !strings.Contains(string(body), "html") {
			t.Fatal("Response doesn't contain HTML content")
		}
	}

	t.Logf("Successfully called endpoint %s", endpoint)
}

// stopProcess stops the given process gracefully using the process monitor
func stopProcess(t *testing.T, cmd *exec.Cmd, procMon *ProcessMonitor, tc *TestContext) {
	t.Helper()

	if cmd == nil || cmd.Process == nil {
		return
	}

	// Send SIGINT for graceful shutdown
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		tc.conditionalLogf(t, "Failed to send SIGINT: %v", err)
		// Force kill if graceful shutdown fails
		if killErr := cmd.Process.Kill(); killErr != nil {
			tc.conditionalLogf(t, "Failed to force kill process: %v", killErr)
		}
	}

	// Wait for process to exit using the shared process monitor
	select {
	case err := <-procMon.Done():
		if err != nil {
			tc.conditionalLogf(t, "Process exited with error: %v", err)
		} else {
			tc.conditionalLogf(t, "Process stopped gracefully")
		}
	case <-time.After(3 * time.Second):
		tc.conditionalLogf(t, "Process did not stop gracefully, forcing kill")
		if killErr := cmd.Process.Kill(); killErr != nil {
			tc.conditionalLogf(t, "Failed to force kill process: %v", killErr)
		}
		// Wait a bit more for the process to actually exit after kill
		select {
		case err := <-procMon.Done():
			tc.conditionalLogf(t, "Process killed, exit error: %v", err)
		case <-time.After(1 * time.Second):
			tc.conditionalLogf(t, "Process kill completed")
		}
	}
}

// removeWippyDir removes the .wippy directory if it exists
func removeWippyDir(t *testing.T) {
	t.Helper()

	projectRoot := getProjectRoot(t)
	wippyPath := filepath.Join(projectRoot, wippyDir)
	if err := os.RemoveAll(wippyPath); err != nil {
		t.Fatalf("Failed to remove .wippy directory: %v", err)
	}
	t.Log("Removed .wippy directory")
}

// removeLockFile removes the wippy.lock file for clean test starts
func removeLockFile(t *testing.T, lockFilePath string) {
	t.Helper()

	projectRoot := getProjectRoot(t)
	fullLockPath := filepath.Join(projectRoot, lockFilePath)
	err := os.Remove(fullLockPath)
	if err != nil && !os.IsNotExist(err) {
		t.Logf("Warning: Failed to remove lock file %s: %v", fullLockPath, err)
	} else if err == nil {
		t.Logf("Removed lock file: %s", fullLockPath)
	}
}

// checkLogContains checks if the log file contains the expected string, waiting with timeout
func checkLogContains(t *testing.T, logFile string, expectedText string) {
	t.Helper()

	timeout := 30 * time.Second // Give enough time for log messages to appear
	checkInterval := time.Second

	t.Logf("Waiting for expected text in log file %s: %s (timeout: %v)", logFile, expectedText, timeout)

	startTime := time.Now()
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	timeoutTimer := time.After(timeout)

	for {
		select {
		case <-timeoutTimer:
			// Final attempt to read the log and provide detailed error
			content, err := os.ReadFile(logFile)
			if err != nil {
				t.Fatalf("Timeout waiting for expected text in log file %s. Failed to read file: %v", logFile, err)
			}
			logContent := string(content)
			t.Fatalf("Timeout (%v) waiting for expected text in log file %s: %s\nActual log content:\n%s",
				timeout, logFile, expectedText, logContent)

		case <-ticker.C:
			content, err := os.ReadFile(logFile)
			if err != nil {
				// Log file might not exist yet, continue waiting
				t.Logf("Log file %s not yet available, continuing to wait... (elapsed: %v)",
					logFile, time.Since(startTime).Round(100*time.Millisecond))
				continue
			}

			logContent := string(content)
			if strings.Contains(logContent, expectedText) {
				elapsed := time.Since(startTime)
				t.Logf("Found expected text in log after %v: %s",
					elapsed.Round(100*time.Millisecond), expectedText)
				return
			}

			// Log progress every 2 seconds to show we're still trying
			elapsed := time.Since(startTime)
			if elapsed.Truncate(2*time.Second) == elapsed {
				t.Logf("Still waiting for expected text in log file %s (elapsed: %v)...",
					logFile, elapsed.Round(100*time.Millisecond))
			}
		}
	}
}

// checkWippyLockExists checks if wippy.lock file exists
func checkWippyLockExists(t *testing.T) {
	t.Helper()

	projectRoot := getProjectRoot(t)
	lockPath := filepath.Join(projectRoot, "app", "wippy.lock")
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatal("wippy.lock file does not exist")
	}

	t.Log("wippy.lock file exists")
}

// TestFirstScenario tests the first scenario: clean start, module loading, and API calls
func TestFirstScenario(t *testing.T) {
	tc := NewTestContext()
	t.Log("Starting Test 1: Clean start and module verification")

	// Step 0: Remove .wippy directory and lock file for truly clean start
	removeWippyDir(t)
	removeLockFile(t, "app/wippy.lock")

	// Step 1: Run the application
	cmd, logFile, err := runCommand(t, "go", []string{"run", "./cmd/runner", "-v", "app/"}, "test.log")
	if err != nil {
		t.Fatalf("Failed to setup command: %v", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatalf("Failed to start command: %v", err)
	}

	// Give the process a moment to start and create the log file
	time.Sleep(100 * time.Millisecond)

	// Step 2: Wait for server to start and get process monitor (reading from log file)
	procMon := waitForServerStartWithLogFile(t, cmd, "test.log")

	// Ensure we stop the process at the end and close the log file
	defer func() {
		stopProcess(t, cmd, procMon, tc)
		logFile.Close()
	}()

	// Step 3: Check that modules are loaded and application started successfully
	checkLogContains(t, "test.log", "application started successfully")

	// Step 4: Check that .wippy contains expected modules
	checkModulesExist(t)

	// Step 5: Make API calls to available endpoints
	checkAPIEndpoint(t, "/") // Check static content is served

	tc.markTestCompleted()
	t.Log("Test 1 completed successfully")
}

// TestUpdateScenario tests the second scenario: module updates
func TestUpdateScenario(t *testing.T) {
	tc := NewTestContext()
	t.Log("Starting Test 2: Module updates")

	projectRoot := getProjectRoot(t)

	// Step 1: Run update command
	cmd := exec.Command("go", "run", "./cmd/runner", "-v", "--update", "app/")
	cmd.Dir = projectRoot

	// Capture stderr to file
	logFile, err := os.Create("test2.log")
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}
	defer logFile.Close()

	cmd.Stderr = logFile

	if err := cmd.Run(); err != nil {
		t.Logf("Update command failed (this might be expected): %v", err)
		// Don't fail here - command might exit with error but still produce useful output
	}

	// Step 2: Check log for module updates
	checkLogContains(t, "test2.log", "Updating dependencies")

	// Step 3: Check that wippy.lock was created
	checkWippyLockExists(t)

	tc.markTestCompleted()
	t.Log("Test 2 completed successfully")
}

// TestInstallScenario tests the third scenario: clean install
func TestInstallScenario(t *testing.T) {
	tc := NewTestContext()
	t.Log("Starting Test 3: Clean install")

	projectRoot := getProjectRoot(t)

	// Step 0: Remove .wippy directory (but keep lock file for install command)
	removeWippyDir(t)

	// Step 1: Run install command
	cmd := exec.Command("go", "run", "./cmd/runner", "-v", "--install", "app/")
	cmd.Dir = projectRoot

	// Capture stderr to file
	logFile, err := os.Create("test3.log")
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}
	defer logFile.Close()

	cmd.Stderr = logFile

	if err := cmd.Run(); err != nil {
		t.Logf("Install command failed (this might be expected): %v", err)
		// Don't fail here - command might exit with error but still produce useful output
	}

	// Step 2: Check log for module installation
	checkLogContains(t, "test3.log", "Installing dependencies")

	// Step 3: Check that .wippy contains expected modules
	checkModulesExist(t)

	tc.markTestCompleted()
	t.Log("Test 3 completed successfully")
}

// TestLockFileScenario tests the fourth scenario: running with clean modules and checking server startup
func TestLockFileScenario(t *testing.T) {
	tc := NewTestContext()
	t.Log("Starting Test 4: Running with fresh modules and checking server startup")

	// Step 1: Run the application
	cmd, logFile, err := runCommand(t, "go", []string{"run", "./cmd/runner", "-v", "app/"}, "test4.log")
	if err != nil {
		t.Fatalf("Failed to setup command: %v", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatalf("Failed to start command: %v", err)
	}

	// Give the process a moment to start and create the log file
	time.Sleep(100 * time.Millisecond)

	// Step 2: Wait for server to start and get process monitor (reading from log file)
	procMon := waitForServerStartWithLogFile(t, cmd, "test4.log")

	// Ensure we stop the process at the end and close the log file
	defer func() {
		stopProcess(t, cmd, procMon, tc)
		logFile.Close()
	}()

	// Step 3: Check that modules are loaded and application started successfully
	checkLogContains(t, "test4.log", "application started successfully")

	// Step 4: Make API calls to available endpoints
	checkAPIEndpoint(t, "/") // Check static content is served

	tc.markTestCompleted()
	t.Log("Test 4 completed successfully")
}
