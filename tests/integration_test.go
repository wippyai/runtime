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
	return 20 * time.Second // Fast timeout for local development
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

// waitForServerStartWithAllPipes waits for server to start by reading both stderr and stdout via channel-based approach
func waitForServerStartWithAllPipes(rootCtx context.Context, t *testing.T, cmd *exec.Cmd) *ProcessMonitor {
	t.Helper()

	ctx, cancel := context.WithCancel(rootCtx)
	defer cancel()

	// Create both stderr and stdout pipes
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("Failed to create stderr pipe: %v", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}

	procMon := NewProcessMonitor(cmd)

	// Buffered channels for communication to avoid blocking
	logLines := make(chan string, 100)     // Buffer for log lines from both stdout/stderr
	serverReady := make(chan bool, 1)      // Server ready notification
	readerDone := make(chan error, 1)      // Reader completion/error
	readersCompleted := make(chan bool, 1) // Both readers finished

	analyzer := NewLogAnalyzer()
	var collectedLogs []string
	var logsMutex sync.Mutex
	var serverReadySignaled int64 // Use atomic for thread safety

	t.Logf("Waiting for server to start (timeout: %v) by reading stdout and stderr", testTimeout)

	// Start the command asynchronously
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	// Start monitoring process exit status in background
	go func() {
		procMon.Start()
	}()

	// Use WaitGroup to properly track when both readers are done
	var readerWg sync.WaitGroup
	readerWg.Add(2)

	// Reader for stderr
	go func() {
		defer readerWg.Done()
		defer cancel()

		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			case logLines <- "[STDERR] " + scanner.Text():
			default:
				// If channel is full, skip this line to avoid blocking
			}
		}
		if err := scanner.Err(); err != nil {
			t.Logf("Error reading stderr: %v", err)
		}
	}()

	// Reader for stdout
	go func() {
		defer readerWg.Done()
		defer cancel()

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			case logLines <- "[STDOUT] " + scanner.Text():
			default:
				// If channel is full, skip this line to avoid blocking
			}
		}
		if err := scanner.Err(); err != nil {
			t.Logf("Error reading stdout: %v", err)
		}
	}()

	// Monitor when both readers complete
	go func() {
		defer cancel()

		readerWg.Wait() // Wait for both readers to finish
		close(logLines) // Close the channel to signal completion
		readersCompleted <- true
	}()

	// Process log lines as they come in from both readers
	go func() {
		defer close(readerDone)
		defer cancel()

		for {
			select {
			case <-ctx.Done():
				return
			case line, ok := <-logLines:
				if !ok {
					// Channel closed, readers are done
					// Final server ready check if not already signaled
					if atomic.LoadInt64(&serverReadySignaled) == 0 {
						if isServerReady(ctx) {
							select {
							case serverReady <- true:
								atomic.StoreInt64(&serverReadySignaled, 1)
								t.Log("Server is ready and responding to HTTP requests")
								return
							default:
							}
						} else {
							logOutput := ""
							logsMutex.Lock()
							if len(collectedLogs) > 0 {
								logOutput = "\n=== SERVER LOGS ===\n" + strings.Join(collectedLogs, "\n") + "\n=== END LOGS ==="
							}
							logsMutex.Unlock()
							select {
							case readerDone <- fmt.Errorf("server logs ended but server is not responding to HTTP requests%s", logOutput):
							default:
							}
						}
					}
				}

				// Extract original line without prefix for analysis
				originalLine := line
				if strings.HasPrefix(line, "[STDERR] ") {
					originalLine = strings.TrimPrefix(line, "[STDERR] ")
				} else if strings.HasPrefix(line, "[STDOUT] ") {
					originalLine = strings.TrimPrefix(line, "[STDOUT] ")
				}

				// Collect for potential error reporting (thread-safe)
				logsMutex.Lock()
				collectedLogs = append(collectedLogs, line)
				logsMutex.Unlock()

				// Log the line
				t.Logf("Server log: %s", originalLine)

				// Analyze line in real-time
				isFailure, isSuccess, message := analyzer.AnalyzeLine(originalLine)
				if isFailure {
					select {
					case readerDone <- fmt.Errorf("server startup failed: %s", message):
					default:
					}
					return
				}
				if isSuccess {
					t.Logf("Success indicator detected: %s", message)
				}
			}
		}
	}()

	// Periodic HTTP health check goroutine independent of log reading
	go func() {
		defer cancel()

		ticker := time.NewTicker(500 * time.Millisecond) // Check every 500ms
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-readersCompleted:
				return // Stop health checks when readers are done
			case <-ticker.C:
				// Check if server is responding via HTTP (but only signal once)
				if atomic.LoadInt64(&serverReadySignaled) == 0 && isServerReady(ctx) {
					select {
					case serverReady <- true:
						atomic.StoreInt64(&serverReadySignaled, 1)
						t.Log("Server is ready and responding to HTTP requests")
						return // Exit this goroutine once server is ready
					default:
					}
				}
			}
		}
	}()

	// Wait for either server ready, process exit, reader error, or timeout
	serverIsReady := false

	for {
		select {
		case err := <-procMon.Done():
			if err != nil {
				t.Fatalf("Server process exited with error: %v", err)
			} else {
				t.Log("Server process exited successfully")
				return procMon
			}
		case <-serverReady:
			if !serverIsReady {
				serverIsReady = true
				t.Log("Server started successfully, continuing to read logs...")
			}
		case err := <-readerDone:
			if err != nil {
				if serverIsReady {
					// Server was ready but reader encountered error - probably normal shutdown
					t.Logf("Log reader finished with error (may be normal): %v", err)
					return procMon
				}
				t.Fatalf("Error waiting for server: %v", err)
			}
			t.Log("Log reader finished successfully")
			return procMon
		case <-rootCtx.Done():
			if serverIsReady {
				t.Log("Server was ready, returning successfully")
				return procMon
			} else {
				logOutput := ""
				logsMutex.Lock()
				if len(collectedLogs) > 0 {
					logOutput = "\n=== SERVER LOGS ===\n" + strings.Join(collectedLogs, "\n") + "\n=== END LOGS ==="
				} else {
					logOutput = "\n=== NO LOGS COLLECTED ===\nNo output was captured from the process\n=== END STATUS ==="
				}
				logsMutex.Unlock()

				t.Fatalf("timeout waiting for server to start after %v%s", testTimeout, logOutput)
			}
		}
	}
}

// isServerReady checks if the server is responding to HTTP requests
func isServerReady(ctx context.Context) bool {
	// Create a short timeout context from the parent context
	timeoutCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, "GET", serverURL, nil)
	if err != nil {
		return false
	}

	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()

	return resp.StatusCode == http.StatusOK
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
func checkAPIEndpoint(ctx context.Context, t *testing.T, endpoint string) {
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

	// Create request with timeout from test context
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, "GET", fullURL, nil)
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
	if cmd.Process != nil {
		if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
			tc.conditionalLogf(t, "Failed to send SIGINT: %v", err)
			// Force kill if graceful shutdown fails
			if killErr := cmd.Process.Kill(); killErr != nil {
				tc.conditionalLogf(t, "Failed to force kill process: %v", killErr)
			}
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
		if cmd.Process != nil {
			if killErr := cmd.Process.Kill(); killErr != nil {
				tc.conditionalLogf(t, "Failed to force kill process: %v", killErr)
			}
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

// checkLogsContain checks if collected logs contain the expected text with timeout
func checkLogsContain(t *testing.T, logs []string, expectedText string) {
	t.Helper()

	timeout := 30 * time.Second // Give enough time for log messages to appear
	checkInterval := 100 * time.Millisecond

	t.Logf("Waiting for expected text in collected logs: %s (timeout: %v)", expectedText, timeout)

	startTime := time.Now()
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	timeoutTimer := time.After(timeout)

	for {
		select {
		case <-timeoutTimer:
			// Final attempt - show all collected logs
			logContent := strings.Join(logs, "\n")
			t.Fatalf("Timeout (%v) waiting for expected text: %s\nActual log content:\n%s",
				timeout, expectedText, strings.TrimSpace(logContent))

		case <-ticker.C:
			// Check if any log line contains the expected text
			for _, line := range logs {
				if strings.Contains(line, expectedText) {
					elapsed := time.Since(startTime)
					t.Logf("Found expected text in log after %v: %s",
						elapsed.Round(100*time.Millisecond), expectedText)
					return
				}
			}

			// Log progress every 2 seconds to show we're still trying
			elapsed := time.Since(startTime)
			if elapsed.Truncate(2*time.Second) == elapsed {
				t.Logf("Still waiting for expected text in collected logs (elapsed: %v)...",
					elapsed.Round(100*time.Millisecond))
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
	ctx, cancel := context.WithTimeout(t.Context(), getTestTimeout())
	defer cancel()

	tc := NewTestContext()
	t.Log("Starting Test 1: Clean start and module verification")

	// Step 0: Remove .wippy directory and lock file for truly clean start
	removeWippyDir(t)
	removeLockFile(t, "app/wippy.lock")

	// Step 1: Run the application (start long-running server)
	projectRoot := getProjectRoot(t)

	cmd := exec.Command("go", "run", "./cmd/runner", "-v", "app/")
	cmd.Dir = projectRoot

	// Step 2: Wait for server to start and get process monitor (reading stderr directly)
	procMon := waitForServerStartWithAllPipes(ctx, t, cmd)

	// Ensure we stop the process at the end
	defer func() {
		stopProcess(t, cmd, procMon, tc)
	}()

	// Step 3: Check that modules are loaded and application started successfully
	// Check collected logs for application started message
	// Note: For TestFirstScenario, logs are collected by waitForServerStartWithAllPipes
	t.Log("Server successfully completed startup - logs were processed in real-time")

	// Step 4: Check that .wippy contains expected modules
	checkModulesExist(t)

	// Step 5: Make API calls to available endpoints
	checkAPIEndpoint(ctx, t, "/") // Check static content is served

	tc.markTestCompleted()
	t.Log("Test 1 completed successfully")
}

// TestUpdateScenario tests the second scenario: module updates
func TestUpdateScenario(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	tc := NewTestContext()
	t.Log("Starting Test 2: Module updates")

	projectRoot := getProjectRoot(t)

	// Step 1: Run update command
	cmd := exec.Command("go", "run", "./cmd/runner", "-v", "--update", "app/")
	cmd.Dir = projectRoot

	// Capture both stderr and stdout with pipes for in-memory processing
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("Failed to create stderr pipe: %v", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}

	// Channel to collect all output
	var collectedLogs []string
	var mu sync.Mutex
	var readerWg sync.WaitGroup
	readerWg.Add(2)

	// Start goroutine to read stderr
	go func() {
		defer readerWg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			t.Logf("Update log: %s", line)

			// Thread-safe append to logs
			mu.Lock()
			collectedLogs = append(collectedLogs, "[STDERR] "+line)
			mu.Unlock()
		}
		if err := scanner.Err(); err != nil {
			t.Logf("Error reading stderr: %v", err)
		}
	}()

	// Start goroutine to read stdout
	go func() {
		defer readerWg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			t.Logf("Update stdout: %s", line)

			// Thread-safe append to logs
			mu.Lock()
			collectedLogs = append(collectedLogs, "[STDOUT] "+line)
			mu.Unlock()
		}
		if err := scanner.Err(); err != nil {
			t.Logf("Error reading stdout: %v", err)
		}
	}()

	// Check if context is already canceled before starting
	select {
	case <-ctx.Done():
		t.Fatalf("Context canceled before starting update command: %v", ctx.Err())
	default:
	}

	// Start command asynchronously to enable stderr reading
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start update command: %v", err)
	}

	// Wait for command to finish
	if err := cmd.Wait(); err != nil {
		t.Logf("Update command failed (this might be expected): %v", err)
		// Don't fail here - command might exit with error but still produce useful output
	}

	// Wait for both readers to complete
	readerWg.Wait()

	// Step 2: Check collected logs for module updates (thread-safe read)
	mu.Lock()
	logsCopy := make([]string, len(collectedLogs))
	copy(logsCopy, collectedLogs)
	mu.Unlock()

	checkLogsContain(t, logsCopy, "Updating dependencies")

	// Step 3: Check that wippy.lock was created
	checkWippyLockExists(t)

	tc.markTestCompleted()
	t.Log("Test 2 completed successfully")
}

// TestInstallScenario tests the third scenario: clean install
func TestInstallScenario(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), getTestTimeout())
	defer cancel()

	tc := NewTestContext()
	t.Log("Starting Test 3: Clean install")

	projectRoot := getProjectRoot(t)

	// Step 0: Remove .wippy directory (but keep lock file for install command)
	removeWippyDir(t)

	// Step 1: Run install command
	cmd := exec.Command("go", "run", "./cmd/runner", "-v", "--install", "app/")
	cmd.Dir = projectRoot

	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()

	// Capture both stderr and stdout with pipes for in-memory processing
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("Failed to create stderr pipe: %v", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}

	// Channel to collect all output
	var collectedLogs []string
	var mu sync.Mutex
	var readerWg sync.WaitGroup
	readerWg.Add(2)

	// Check if context is already canceled before starting
	select {
	case <-ctx.Done():
		t.Fatalf("Context canceled before starting install command: %v", ctx.Err())
	default:
	}

	// Start goroutine to read stderr
	go func() {
		defer readerWg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			t.Logf("Install log: %s", line)

			// Thread-safe append to logs
			mu.Lock()
			collectedLogs = append(collectedLogs, "[STDERR] "+line)
			mu.Unlock()
		}
		if err := scanner.Err(); err != nil {
			t.Logf("Error reading stderr: %v", err)
		}
	}()

	// Start goroutine to read stdout
	go func() {
		defer readerWg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			t.Logf("Install stdout: %s", line)

			// Thread-safe append to logs
			mu.Lock()
			collectedLogs = append(collectedLogs, "[STDOUT] "+line)
			mu.Unlock()
		}
		if err := scanner.Err(); err != nil {
			t.Logf("Error reading stdout: %v", err)
		}
	}()

	// Start command asynchronously to enable stderr reading
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start install command: %v", err)
	}

	// Wait for command to finish
	if err := cmd.Wait(); err != nil {
		t.Logf("Install command failed (this might be expected): %v", err)
		// Don't fail here - command might exit with error but still produce useful output
	}

	// Wait for both readers to complete
	readerWg.Wait()

	// Step 2: Check collected logs for module installation (thread-safe read)
	mu.Lock()
	logsCopy := make([]string, len(collectedLogs))
	copy(logsCopy, collectedLogs)
	mu.Unlock()

	checkLogsContain(t, logsCopy, "Installing dependencies")

	// Step 3: Check that .wippy contains expected modules
	checkModulesExist(t)

	tc.markTestCompleted()
	t.Log("Test 3 completed successfully")
}

// TestLockFileScenario tests the fourth scenario: running with clean modules and checking server startup
func TestLockFileScenario(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), getTestTimeout())
	defer cancel()

	tc := NewTestContext()
	t.Log("Starting Test 4: Running with fresh modules and checking server startup")

	// Step 1: Run the application
	projectRoot := getProjectRoot(t)

	cmd := exec.Command("go", "run", "./cmd/runner", "-v", "app/")
	cmd.Dir = projectRoot

	// Step 2: Wait for server to start and get process monitor (reading stderr directly)
	procMon := waitForServerStartWithAllPipes(ctx, t, cmd)

	// Ensure we stop the process at the end
	defer func() {
		stopProcess(t, cmd, procMon, tc)
	}()

	// Step 3: Check that modules are loaded and application started successfully
	// Check collected logs for application started message
	// Note: For TestLockFileScenario, logs are collected by waitForServerStartWithAllPipes
	t.Log("Server successfully completed startup - logs were processed in real-time")

	// Step 4: Make API calls to available endpoints
	checkAPIEndpoint(ctx, t, "/") // Check static content is served

	tc.markTestCompleted()
	t.Log("Test 4 completed successfully")
}
