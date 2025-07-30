package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
)

// Configuration holds the test runner configuration
type Configuration struct {
	BaseURL       string
	Timeout       time.Duration
	SkipCheck     bool
	Verbose       bool
	ReportFile    string
	MaxAttempts   int
	RetryInterval time.Duration
}

// TestResult represents the result of a test run
type TestResult struct {
	Success   bool          `json:"success"`
	Duration  time.Duration `json:"duration"`
	Error     string        `json:"error,omitempty"`
	Output    string        `json:"output,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}

// TestReport represents the complete test report
type TestReport struct {
	Configuration Configuration `json:"configuration"`
	Results       TestResult    `json:"results"`
	Summary       struct {
		TotalTests    int           `json:"total_tests"`
		PassedTests   int           `json:"passed_tests"`
		FailedTests   int           `json:"failed_tests"`
		SuccessRate   float64       `json:"success_rate"`
		TotalDuration time.Duration `json:"total_duration"`
	} `json:"summary"`
}

// Colors for output
var (
	red    = color.New(color.FgRed)
	green  = color.New(color.FgGreen)
	yellow = color.New(color.FgYellow)
	blue   = color.New(color.FgBlue)
	bold   = color.New(color.Bold)
)

func main() {
	config := parseFlags()

	// Print banner
	printBanner()

	// Check prerequisites
	if err := checkPrerequisites(); err != nil {
		logError("Prerequisites check failed: %v", err)
		os.Exit(1)
	}

	// Check if application is running (unless skipped)
	if !config.SkipCheck {
		if err := checkAppRunning(config); err != nil {
			logError("Application check failed: %v", err)
			os.Exit(1)
		}
	} else {
		logWarning("Skipping application availability check")
	}

	// Run tests
	report, err := runTests(config)
	if err != nil {
		logError("Test execution failed: %v", err)
		os.Exit(1)
	}

	// Print results
	printResults(report)

	// Save report
	if err := saveReport(report, config.ReportFile); err != nil {
		logError("Failed to save report: %v", err)
		os.Exit(1)
	}

	// Exit with appropriate code
	if report.Results.Success {
		logSuccess("Test execution completed successfully")
		os.Exit(0)
	} else {
		logError("Test execution failed")
		os.Exit(1)
	}
}

func parseFlags() Configuration {
	var (
		baseURL       = flag.String("url", "http://localhost:8082", "Base URL for the application")
		timeout       = flag.Duration("timeout", 5*time.Minute, "Timeout for test execution")
		skipCheck     = flag.Bool("skip-check", false, "Skip application availability check")
		verbose       = flag.Bool("verbose", false, "Enable verbose output")
		reportFile    = flag.String("report", "comprehensive_e2e_report.json", "Report file path")
		maxAttempts   = flag.Int("max-attempts", 30, "Maximum attempts for app availability check")
		retryInterval = flag.Duration("retry-interval", 2*time.Second, "Retry interval for app availability check")
	)

	flag.Parse()

	return Configuration{
		BaseURL:       *baseURL,
		Timeout:       *timeout,
		SkipCheck:     *skipCheck,
		Verbose:       *verbose,
		ReportFile:    *reportFile,
		MaxAttempts:   *maxAttempts,
		RetryInterval: *retryInterval,
	}
}

func printBanner() {
	fmt.Println("==========================================")
	bold.Println("  Comprehensive E2E Test Runner (Pure Go)")
	fmt.Println("==========================================")
	fmt.Println()
}

func checkPrerequisites() error {
	logInfo("Checking prerequisites...")

	// Check if Go is installed
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("Go is not installed or not in PATH: %v", err)
	}

	// Get Go version
	cmd := exec.Command("go", "version")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("Failed to get Go version: %v", err)
	}

	logInfo("Go version: %s", strings.TrimSpace(string(output)))

	// Check if we're in the right directory (should have go.mod)
	if _, err := os.Stat("go.mod"); os.IsNotExist(err) {
		return fmt.Errorf("go.mod not found. Please run from the project root directory")
	}

	logSuccess("Prerequisites check passed")
	return nil
}

func checkAppRunning(config Configuration) error {
	logInfo("Checking if application is running at %s...", config.BaseURL)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		if config.Verbose {
			logInfo("Attempt %d/%d: Checking application availability...", attempt, config.MaxAttempts)
		}

		resp, err := client.Get(config.BaseURL + "/api/v1/hello")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			logSuccess("Application is running and responding")
			return nil
		}

		if err != nil {
			if config.Verbose {
				logWarning("Attempt %d/%d: Connection failed: %v", attempt, config.MaxAttempts, err)
			}
		} else {
			resp.Body.Close()
			if config.Verbose {
				logWarning("Attempt %d/%d: Application responded with status: %d", attempt, config.MaxAttempts, resp.StatusCode)
			}
		}

		if attempt < config.MaxAttempts {
			logWarning("Waiting %v before next attempt...", config.RetryInterval)
			time.Sleep(config.RetryInterval)
		}
	}

	return fmt.Errorf("application is not responding after %d attempts", config.MaxAttempts)
}

func runTests(config Configuration) (*TestReport, error) {
	logInfo("Running comprehensive e2e tests...")
	logInfo("Base URL: %s", config.BaseURL)
	logInfo("Timeout: %v", config.Timeout)

	// Set environment variable for the test
	os.Setenv("TEST_BASE_URL", config.BaseURL)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	// Prepare command
	cmd := exec.CommandContext(ctx, "go", "test", "-v", "-timeout", config.Timeout.String(), "./tests/e2e/comprehensive")

	// Capture output
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Start timing
	start := time.Now()

	// Run the command
	err := cmd.Run()

	// Calculate duration
	duration := time.Since(start)

	// Prepare result
	result := TestResult{
		Duration:  duration,
		Timestamp: time.Now(),
		Output:    stdout.String(),
	}

	if err != nil {
		result.Success = false
		result.Error = err.Error()
		if stderr.Len() > 0 {
			result.Error += "\nStderr: " + stderr.String()
		}
	} else {
		result.Success = true
	}

	// Create report
	report := &TestReport{
		Configuration: config,
		Results:       result,
	}

	// Calculate summary
	calculateSummary(report)

	return report, nil
}

func calculateSummary(report *TestReport) {
	output := report.Results.Output

	// Count test results from output
	lines := strings.Split(output, "\n")
	totalTests := 0
	passedTests := 0
	failedTests := 0

	for _, line := range lines {
		if strings.Contains(line, "PASS:") {
			passedTests++
			totalTests++
		} else if strings.Contains(line, "FAIL:") {
			failedTests++
			totalTests++
		} else if strings.Contains(line, "RUN:") {
			// This is a test being run
		}
	}

	// If we couldn't parse the output, make reasonable assumptions
	if totalTests == 0 {
		if report.Results.Success {
			totalTests = 1
			passedTests = 1
		} else {
			totalTests = 1
			failedTests = 1
		}
	}

	report.Summary.TotalTests = totalTests
	report.Summary.PassedTests = passedTests
	report.Summary.FailedTests = failedTests
	report.Summary.TotalDuration = report.Results.Duration

	if totalTests > 0 {
		report.Summary.SuccessRate = float64(passedTests) / float64(totalTests)
	}
}

func printResults(report *TestReport) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 80))
	bold.Println("COMPREHENSIVE E2E TEST REPORT")
	fmt.Println(strings.Repeat("=", 80))

	// Configuration
	fmt.Printf("Base URL: %s\n", report.Configuration.BaseURL)
	fmt.Printf("Timeout: %v\n", report.Configuration.Timeout)
	fmt.Printf("Timestamp: %s\n", report.Results.Timestamp.Format(time.RFC3339))
	fmt.Println()

	// Summary
	fmt.Printf("Total Tests: %d\n", report.Summary.TotalTests)
	fmt.Printf("Passed: %d\n", report.Summary.PassedTests)
	fmt.Printf("Failed: %d\n", report.Summary.FailedTests)
	fmt.Printf("Success Rate: %.2f%%\n", report.Summary.SuccessRate*100)
	fmt.Printf("Total Duration: %v\n", report.Summary.TotalDuration)
	fmt.Println()

	// Status
	if report.Results.Success {
		green.Printf("✅ Test execution: SUCCESS\n")
	} else {
		red.Printf("❌ Test execution: FAILED\n")
		if report.Results.Error != "" {
			red.Printf("Error: %s\n", report.Results.Error)
		}
	}

	fmt.Println(strings.Repeat("=", 80))
	fmt.Println()

	// Detailed output (if verbose or failed)
	if report.Configuration.Verbose || !report.Results.Success {
		fmt.Println("DETAILED OUTPUT:")
		fmt.Println(strings.Repeat("-", 80))
		fmt.Println(report.Results.Output)
		fmt.Println(strings.Repeat("-", 80))
	}
}

func saveReport(report *TestReport, filename string) error {
	// Ensure directory exists
	dir := filepath.Dir(filename)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %v", dir, err)
		}
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %v", err)
	}

	// Write to file
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write report file: %v", err)
	}

	logInfo("Report saved to: %s", filename)
	return nil
}

// Logging functions
func logInfo(format string, args ...interface{}) {
	blue.Printf("[INFO] ")
	fmt.Printf(format+"\n", args...)
}

func logSuccess(format string, args ...interface{}) {
	green.Printf("[SUCCESS] ")
	fmt.Printf(format+"\n", args...)
}

func logWarning(format string, args ...interface{}) {
	yellow.Printf("[WARNING] ")
	fmt.Printf(format+"\n", args...)
}

func logError(format string, args ...interface{}) {
	red.Printf("[ERROR] ")
	fmt.Printf(format+"\n", args...)
}
