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

// getTestTimeout returns appropriate timeout based on environment
func getTestTimeout() time.Duration {
	// Check for CI environment
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		return 60 * time.Second // Longer timeout for CI
	}
	return 15 * time.Second // Faster timeout for local development
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

// runCommand executes a command and returns its output and error
func runCommand(t *testing.T, command string, args []string, logFile string) (cmd *exec.Cmd, stderr io.Reader, err error) {
	t.Helper()

	projectRoot := getProjectRoot(t)

	// Change to project root directory
	if err := os.Chdir(projectRoot); err != nil {
		return nil, nil, fmt.Errorf("failed to change directory: %w", err)
	}

	cmd = exec.Command(command, args...)
	cmd.Dir = projectRoot

	// Create log file for stderr
	logFileHandle, err := os.Create(logFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create log file %s: %w", logFile, err)
	}

	// Create a pipe to read stderr while also writing to file
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		logFileHandle.Close()
		return nil, nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Use a MultiWriter to write stderr to both file and our reader
	stderr = io.TeeReader(stderrPipe, logFileHandle)

	return cmd, stderr, nil
}

// waitForServerStart waits for the server to start by checking logs and HTTP endpoint
func waitForServerStart(t *testing.T, stderr io.Reader) {
	t.Helper()

	scanner := bufio.NewScanner(stderr)
	timeout := time.After(testTimeout)
	serverReady := make(chan bool, 1)
	logOutput := make(chan string, 100) // Buffered channel for log lines
	scannerDone := make(chan bool, 1)

	t.Logf("Waiting for server to start (timeout: %v)", testTimeout)

	// Read logs in separate goroutine to prevent blocking
	go func() {
		defer close(scannerDone)
		for scanner.Scan() {
			line := scanner.Text()
			select {
			case logOutput <- line:
			case <-timeout:
				return
			}
		}
		if err := scanner.Err(); err != nil {
			t.Logf("Error reading stderr: %v", err)
		}
	}()

	// Check server readiness via HTTP in parallel
	go func() {
		httpCheckInterval := time.NewTicker(500 * time.Millisecond)
		defer httpCheckInterval.Stop()

		for {
			select {
			case <-timeout:
				return
			case <-httpCheckInterval.C:
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				req, err := http.NewRequestWithContext(ctx, "GET", serverURL, nil)
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

	logCheckTicker := time.NewTicker(50 * time.Millisecond)
	defer logCheckTicker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("Timeout waiting for server to start - server may have failed to start properly")
		case <-serverReady:
			t.Log("Server is ready and responding to HTTP requests")
			// Give more time for modules to be fully loaded
			time.Sleep(2 * time.Second)
			return
		case line := <-logOutput:
			t.Logf("Server log: %s", line)
			if strings.Contains(line, "application started successfully") {
				t.Log("Server started successfully")
			}
			// Check if we see error patterns that indicate server won't start
			if strings.Contains(line, "panic:") || strings.Contains(line, "fatal error:") {
				t.Fatalf("Server failed to start with error: %s", line)
			}
		case <-scannerDone:
			t.Log("Log scanner finished - checking if server is responsive")
			// Scanner is done, give HTTP check a bit more time
			select {
			case <-serverReady:
				t.Log("Server is ready and responding to HTTP requests")
				time.Sleep(2 * time.Second)
				return
			case <-time.After(5 * time.Second):
				t.Fatal("Server logs ended but server is not responding to HTTP requests")
			}
		case <-logCheckTicker.C:
			// Just continue the loop - this prevents busy waiting
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

// waitForServerStartWithProcessMonitoring waits for server start while monitoring process health
func waitForServerStartWithProcessMonitoring(t *testing.T, cmd *exec.Cmd, stderr io.Reader) *ProcessMonitor {
	t.Helper()

	// Create and start process monitor
	procMon := NewProcessMonitor(cmd)
	procMon.Start()

	// Start normal server waiting in background
	serverReady := make(chan bool, 1)
	waitErr := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				waitErr <- fmt.Errorf("panic in waitForServerStart: %v", r)
			}
		}()

		// This will call t.Fatal internally if it times out
		// We need to catch that and convert to channel message
		done := make(chan bool)
		go func() {
			waitForServerStart(t, stderr)
			done <- true
		}()

		<-done
		serverReady <- true
	}()

	// Wait for either server ready or process exit
	select {
	case err := <-procMon.Done():
		if err != nil {
			t.Fatalf("Server process exited with error before starting: %v", err)
		} else {
			t.Fatal("Server process exited successfully before server was ready")
		}
	case <-serverReady:
		t.Log("Server started successfully")
		return procMon
	case err := <-waitErr:
		t.Fatalf("Error waiting for server: %v", err)
	case <-time.After(testTimeout + 10*time.Second):
		t.Fatal("Test timeout exceeded - killing process")
	}
	return nil
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
func stopProcess(t *testing.T, cmd *exec.Cmd, procMon *ProcessMonitor) {
	t.Helper()

	if cmd == nil || cmd.Process == nil {
		return
	}

	// Send SIGINT for graceful shutdown
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Logf("Failed to send SIGINT: %v", err)
		// Force kill if graceful shutdown fails
		if killErr := cmd.Process.Kill(); killErr != nil {
			t.Logf("Failed to force kill process: %v", killErr)
		}
	}

	// Wait for process to exit using the shared process monitor
	select {
	case err := <-procMon.Done():
		if err != nil {
			t.Logf("Process exited with error: %v", err)
		} else {
			t.Log("Process stopped gracefully")
		}
	case <-time.After(3 * time.Second):
		t.Log("Process did not stop gracefully, forcing kill")
		if killErr := cmd.Process.Kill(); killErr != nil {
			t.Logf("Failed to force kill process: %v", killErr)
		}
		// Wait a bit more for the process to actually exit after kill
		select {
		case err := <-procMon.Done():
			t.Logf("Process killed, exit error: %v", err)
		case <-time.After(1 * time.Second):
			t.Log("Process kill completed")
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

// checkLogContains checks if the log file contains the expected string
func checkLogContains(t *testing.T, logFile string, expectedText string) {
	t.Helper()

	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file %s: %v", logFile, err)
	}

	logContent := string(content)
	if !strings.Contains(logContent, expectedText) {
		t.Fatalf("Log file %s does not contain expected text: %s\nLog content:\n%s", logFile, expectedText, logContent)
	}

	t.Logf("Found expected text in log: %s", expectedText)
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
	t.Log("Starting Test 1: Clean start and module verification")

	// Step 0: Remove .wippy directory
	removeWippyDir(t)

	// Step 1: Run the application
	cmd, stderr, err := runCommand(t, "go", []string{"run", "./cmd/runner", "-v", "tests/tree-deps-src/"}, "test.log")
	if err != nil {
		t.Fatalf("Failed to setup command: %v", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	// Step 2: Wait for server to start and get process monitor
	procMon := waitForServerStartWithProcessMonitoring(t, cmd, stderr)

	// Ensure we stop the process at the end
	defer stopProcess(t, cmd, procMon)

	// Step 3: Check that .wippy contains expected modules
	checkModulesExist(t)

	// Step 4 & 5: Make API calls to available endpoints
	checkAPIEndpoint(t, "/") // Check static content is served

	t.Log("Test 1 completed successfully")
}

// TestUpdateScenario tests the second scenario: module updates
func TestUpdateScenario(t *testing.T) {
	t.Log("Starting Test 2: Module updates")

	projectRoot := getProjectRoot(t)

	// Step 1: Run update command
	cmd := exec.Command("go", "run", "./cmd/runner", "-v", "-update", "app/")
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

	t.Log("Test 2 completed successfully")
}

// TestInstallScenario tests the third scenario: clean install
func TestInstallScenario(t *testing.T) {
	t.Log("Starting Test 3: Clean install")

	projectRoot := getProjectRoot(t)

	// Step 0: Remove .wippy directory
	removeWippyDir(t)

	// Step 1: Run install command
	cmd := exec.Command("go", "run", "./cmd/runner", "-v", "-install", "app/")
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

	t.Log("Test 3 completed successfully")
}

// TestLockFileScenario tests the fourth scenario: running with clean modules and checking server startup
func TestLockFileScenario(t *testing.T) {
	t.Log("Starting Test 4: Running with fresh modules and checking server startup")

	// Step 1: Run the application
	cmd, stderr, err := runCommand(t, "go", []string{"run", "./cmd/runner", "-v", "tests/tree-deps-src/"}, "test4.log")
	if err != nil {
		t.Fatalf("Failed to setup command: %v", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	// Step 2: Wait for server to start and get process monitor
	procMon := waitForServerStartWithProcessMonitoring(t, cmd, stderr)

	// Ensure we stop the process at the end
	defer stopProcess(t, cmd, procMon)

	// Step 3: Check that modules are loaded and application started successfully
	checkLogContains(t, "test4.log", ".env file loaded successfully")

	// Step 4: Make API calls to available endpoints
	checkAPIEndpoint(t, "/") // Check static content is served

	t.Log("Test 4 completed successfully")
}
