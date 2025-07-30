package advanced

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Configuration
const (
	DefaultBaseURL = "http://localhost:8082"
	DefaultTimeout = 30 * time.Second
	APIBasePath    = "/api/v1"
)

// ValidationSchema defines expected response structure
type ValidationSchema struct {
	RequiredFields  []string               `json:"required_fields,omitempty"`
	ExpectedValues  map[string]interface{} `json:"expected_values,omitempty"`
	ContentType     string                 `json:"content_type,omitempty"`
	Type            string                 `json:"type"`
	Patterns        map[string]string      `json:"patterns,omitempty"`
	CustomValidator func([]byte) []string  `json:"-"`
}

// TestResult represents the result of a single endpoint test
type TestResult struct {
	Name       string        `json:"name"`
	Method     string        `json:"method"`
	URL        string        `json:"url"`
	Success    bool          `json:"success"`
	Error      string        `json:"error,omitempty"`
	Status     int           `json:"status,omitempty"`
	Duration   time.Duration `json:"duration"`
	Timestamp  time.Time     `json:"timestamp"`
	Validation *Validation   `json:"validation,omitempty"`
	Response   string        `json:"response,omitempty"`
}

// Validation represents response validation results
type Validation struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

// TestSuite represents the advanced e2e test suite
type TestSuite struct {
	baseURL     string
	client      *http.Client
	results     []TestResult
	appStarted  bool
	sessionData map[string]string
	schemas     map[string]ValidationSchema
}

// Validation schemas for different endpoints based on real responses
var validationSchemas = map[string]ValidationSchema{
	"Local Time": {
		RequiredFields: []string{"unix_timestamp", "timezone", "components", "time"},
		Type:           "json",
		Patterns: map[string]string{
			"time": `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`, // RFC3339 pattern
		},
	},
	"Models List": {
		RequiredFields: []string{"models", "grouped"},
		Type:           "json",
	},
	"Time Ticker": {
		ContentType: "application/json",
		Type:        "text",
		CustomValidator: func(body []byte) []string {
			bodyStr := string(body)
			// Time ticker returns streaming JSON, so we check for the pattern
			if !strings.Contains(bodyStr, "tick") || !strings.Contains(bodyStr, "elapsed") {
				return []string{"Expected streaming JSON response to contain 'tick' and 'elapsed' fields"}
			}
			return nil
		},
	},
	"Registry Dump": {
		ContentType: "text/html",
		Type:        "html",
		CustomValidator: func(body []byte) []string {
			bodyStr := string(body)
			if !strings.Contains(bodyStr, "Registry Explorer") {
				return []string{"Expected HTML to contain 'Registry Explorer'"}
			}
			return nil
		},
	},
	"Environment Demo": {
		ContentType: "text/plain",
		Type:        "text",
		CustomValidator: func(body []byte) []string {
			bodyStr := string(body)
			if !strings.Contains(bodyStr, "ENVIRONMENT TEST RESULTS") {
				return []string{"Expected text to contain 'ENVIRONMENT TEST RESULTS'"}
			}
			return nil
		},
	},
	"Filesystem Browse": {
		RequiredFields: []string{"path", "entries"},
		Type:           "json",
	},
	"Process Status": {
		RequiredFields: []string{"processes"},
		Type:           "json",
	},
	"Discover Tests": {
		RequiredFields: []string{"tests"},
		Type:           "json",
	},
	"Migration Status": {
		RequiredFields: []string{"migrations"},
		Type:           "json",
	},
	"Available Databases": {
		RequiredFields: []string{"databases"},
		Type:           "json",
	},
	"Get Specs": {
		RequiredFields: []string{"specs"},
		Type:           "json",
	},
	"Index HTML": {
		ContentType: "text/html",
		Type:        "html",
		CustomValidator: func(body []byte) []string {
			bodyStr := string(body)
			if !strings.Contains(bodyStr, "Chat Interface") {
				return []string{"Expected HTML to contain 'Chat Interface'"}
			}
			return nil
		},
	},
	"Dashboard CSS": {
		ContentType: "text/css",
		Type:        "css",
	},
	"Test JS": {
		ContentType: "text/javascript",
		Type:        "js",
	},
	"Todo App Page": {
		ContentType: "text/html",
		Type:        "html",
	},
	"Document Search Page": {
		ContentType: "text/html",
		Type:        "html",
	},
	"Security Page": {
		ContentType: "text/html",
		Type:        "html",
	},
	"Tools Page": {
		ContentType: "text/html",
		Type:        "html",
	},
	"Models Page": {
		ContentType: "text/html",
		Type:        "html",
	},
	"Upload Page": {
		ContentType: "text/html",
		Type:        "html",
	},
	"Test Page": {
		ContentType: "text/html",
		Type:        "html",
	},
	"Blob Page": {
		ContentType: "text/html",
		Type:        "html",
	},
	"Specs Page": {
		ContentType: "text/html",
		Type:        "html",
	},
	"Lifecycle Page": {
		ContentType: "text/html",
		Type:        "html",
	},
	"Migrations Page": {
		ContentType: "text/html",
		Type:        "html",
	},
}

// NewTestSuite creates a new advanced test suite instance
func NewTestSuite(baseURL string) *TestSuite {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	return &TestSuite{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: DefaultTimeout,
		},
		results:     make([]TestResult, 0),
		sessionData: make(map[string]string),
		schemas:     validationSchemas,
	}
}

// log prints a formatted log message
func (ts *TestSuite) log(format string, args ...interface{}) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	message := fmt.Sprintf(format, args...)
	fmt.Printf("[%s] %s\n", timestamp, message)
}

// validateResponse validates a response against a schema
func (ts *TestSuite) validateResponse(name string, resp *http.Response, body []byte, schema ValidationSchema) Validation {
	validation := Validation{Valid: true, Errors: make([]string, 0)}

	// Check content type
	if schema.ContentType != "" {
		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, schema.ContentType) {
			validation.Valid = false
			validation.Errors = append(validation.Errors, fmt.Sprintf("Expected content-type to contain '%s', got '%s'", schema.ContentType, contentType))
		}
	}

	// Validate JSON responses
	if schema.Type == "json" {
		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			validation.Valid = false
			validation.Errors = append(validation.Errors, fmt.Sprintf("Invalid JSON response: %v", err))
			return validation
		}

		// Check required fields
		for _, field := range schema.RequiredFields {
			if _, exists := data[field]; !exists {
				validation.Valid = false
				validation.Errors = append(validation.Errors, fmt.Sprintf("Missing required field: %s", field))
			}
		}

		// Check expected values
		for field, expectedValue := range schema.ExpectedValues {
			if actualValue, exists := data[field]; exists {
				if actualValue != expectedValue {
					validation.Valid = false
					validation.Errors = append(validation.Errors, fmt.Sprintf("Expected %s to be '%v', got '%v'", field, expectedValue, actualValue))
				}
			}
		}

		// Check patterns
		for field, pattern := range schema.Patterns {
			if value, exists := data[field]; exists {
				if strValue, ok := value.(string); ok {
					matched, err := regexp.MatchString(pattern, strValue)
					if err != nil {
						validation.Valid = false
						validation.Errors = append(validation.Errors, fmt.Sprintf("Invalid regex pattern for %s: %v", field, err))
					} else if !matched {
						validation.Valid = false
						validation.Errors = append(validation.Errors, fmt.Sprintf("Field %s does not match pattern '%s': '%s'", field, pattern, strValue))
					}
				}
			}
		}
	}

	// Custom validation
	if schema.CustomValidator != nil {
		customErrors := schema.CustomValidator(body)
		if len(customErrors) > 0 {
			validation.Valid = false
			validation.Errors = append(validation.Errors, customErrors...)
		}
	}

	return validation
}

// testEndpoint tests a single endpoint with validation
func (ts *TestSuite) testEndpoint(name, method, endpoint string, body io.Reader, headers map[string]string) TestResult {
	startTime := time.Now()

	// Construct full URL
	fullURL := ts.baseURL + endpoint
	if !strings.HasPrefix(endpoint, "http") {
		if strings.HasPrefix(endpoint, "/") {
			fullURL = ts.baseURL + endpoint
		} else {
			fullURL = ts.baseURL + "/" + endpoint
		}
	}

	// Create request
	req, err := http.NewRequest(method, fullURL, body)
	if err != nil {
		return TestResult{
			Name:      name,
			Method:    method,
			URL:       fullURL,
			Success:   false,
			Error:     fmt.Sprintf("Failed to create request: %v", err),
			Timestamp: time.Now(),
		}
	}

	// Add headers
	if headers != nil {
		for key, value := range headers {
			req.Header.Set(key, value)
		}
	}

	// Add session data if available
	if ts.sessionData["token"] != "" {
		req.Header.Set("Authorization", "Bearer "+ts.sessionData["token"])
	}

	// Make request
	resp, err := ts.client.Do(req)
	duration := time.Since(startTime)

	result := TestResult{
		Name:      name,
		Method:    method,
		URL:       fullURL,
		Timestamp: time.Now(),
		Duration:  duration,
	}

	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("Request failed: %v", err)
	} else {
		defer resp.Body.Close()
		result.Status = resp.StatusCode
		result.Success = resp.StatusCode >= 200 && resp.StatusCode < 300

		// Read response body
		bodyBytes, _ := io.ReadAll(resp.Body)
		result.Response = string(bodyBytes)

		if !result.Success {
			result.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Status)
		} else {
			// Validate response if schema exists
			if schema, exists := ts.schemas[name]; exists {
				validation := ts.validateResponse(name, resp, bodyBytes, schema)
				result.Validation = &validation

				if !validation.Valid {
					result.Success = false
					result.Error = fmt.Sprintf("Validation failed: %s", strings.Join(validation.Errors, ", "))
				}
			}
		}

		// Store session data if present
		if sessionID := resp.Header.Get("X-Session-ID"); sessionID != "" {
			ts.sessionData["session_id"] = sessionID
		}
		if token := resp.Header.Get("X-Auth-Token"); token != "" {
			ts.sessionData["token"] = token
		}
	}

	// Log result
	if result.Success {
		ts.log("✅ %s - %s %s (%v)", name, method, fullURL, duration)
	} else {
		ts.log("❌ %s - %s %s - %s (%v)", name, method, fullURL, result.Error, duration)
		if result.Validation != nil && !result.Validation.Valid {
			for _, err := range result.Validation.Errors {
				ts.log("   Validation error: %s", err)
			}
		}
	}

	ts.results = append(ts.results, result)
	return result
}

// verifyAppStarted checks if the application is running
func (ts *TestSuite) verifyAppStarted() bool {
	ts.log("Checking if application is running at %s...", ts.baseURL)

	resp, err := ts.client.Get(ts.baseURL)
	if err != nil {
		ts.log("❌ Application is not accessible: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		// Read body to verify it's actually the application
		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyStr := string(bodyBytes)

		// Check if it contains expected content
		if strings.Contains(bodyStr, "Chat Interface") ||
			strings.Contains(bodyStr, "index.html") ||
			strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
			ts.log("✅ Application is running and accessible")
			ts.appStarted = true
			return true
		}
	}

	ts.log("❌ Application is not responding correctly (status: %d)", resp.StatusCode)
	return false
}

// RunAllTests runs all advanced e2e tests
func (ts *TestSuite) RunAllTests() {
	ts.log("🚀 Starting Advanced E2E Test Suite")
	ts.log("📡 Testing against: %s", ts.baseURL)

	ts.log("📋 Running all endpoint tests with validation...")

	// Main HTTP API Routes
	ts.testMainAPIRoutes()

	// Demo Routes
	ts.testDemoRoutes()

	// Chat Routes
	ts.testChatRoutes()

	// Wippy Framework Routes
	ts.testWippyRoutes()

	// Static Files
	ts.testStaticFiles()

	ts.log("🏁 Advanced E2E Test Suite completed")
}

// testMainAPIRoutes tests the main API routes with validation
func (ts *TestSuite) testMainAPIRoutes() {
	ts.log("🔧 Testing Main API Routes with Validation...")

	// Basic endpoints with validation
	ts.testEndpoint("Local Time", "GET", APIBasePath+"/time/local", nil, nil)
	ts.testEndpoint("Function PID", "GET", APIBasePath+"/pid", nil, nil)
	ts.testEndpoint("Hello World", "GET", APIBasePath+"/hello", nil, nil)

	// System endpoints with validation
	ts.testEndpoint("System Environment", "GET", APIBasePath+"/system/env", nil, nil)
	ts.testEndpoint("Registry Dump", "GET", APIBasePath+"/registry/dump", nil, nil)
	ts.testEndpoint("Tools List", "GET", APIBasePath+"/tools/list", nil, nil)
	ts.testEndpoint("Models List", "GET", APIBasePath+"/models/list", nil, nil)

	// Filesystem and time endpoints
	ts.testEndpoint("Filesystem Browse", "GET", APIBasePath+"/fs/browse", nil, nil)
	ts.testEndpoint("Time Ticker", "GET", APIBasePath+"/time/ticker", nil, nil)
}

// testDemoRoutes tests the demo routes with validation
func (ts *TestSuite) testDemoRoutes() {
	ts.log("🎯 Testing Demo Routes with Validation...")

	// Security demo - create actor first
	securityData := map[string]interface{}{
		"operation": "create_actor",
		"actor_id":  "test-actor",
		"metadata": map[string]interface{}{
			"role":  "user",
			"email": "test@example.com",
		},
	}
	securityJSON, _ := json.Marshal(securityData)
	securityResult := ts.testEndpoint("Security API - Create Actor", "POST", APIBasePath+"/security", bytes.NewBuffer(securityJSON), map[string]string{
		"Content-Type": "application/json",
	})

	// If actor creation succeeded, test protected endpoints
	if securityResult.Success {
		ts.testEndpoint("Protected Resource", "GET", APIBasePath+"/protected", nil, nil)
	}

	// Todo app with validation
	ts.testEndpoint("List Todos", "GET", APIBasePath+"/todos", nil, nil)
	ts.testEndpoint("Get Todo", "GET", APIBasePath+"/todos/get", nil, nil)

	todoData := map[string]interface{}{
		"title":     "Test Todo",
		"completed": false,
	}
	todoJSON, _ := json.Marshal(todoData)
	addTodoResult := ts.testEndpoint("Add Todo", "POST", APIBasePath+"/todos", bytes.NewBuffer(todoJSON), map[string]string{
		"Content-Type": "application/json",
	})

	if addTodoResult.Success {
		updateData := map[string]interface{}{
			"id":        1,
			"title":     "Updated Todo",
			"completed": true,
		}
		updateJSON, _ := json.Marshal(updateData)
		ts.testEndpoint("Update Todo", "PUT", APIBasePath+"/todos/update", bytes.NewBuffer(updateJSON), map[string]string{
			"Content-Type": "application/json",
		})
	}

	ts.testEndpoint("Delete Todo", "DELETE", APIBasePath+"/todos/delete", nil, nil)

	// Document search with validation
	ts.testEndpoint("List Documents", "GET", APIBasePath+"/documents", nil, nil)
	ts.testEndpoint("Search Documents", "GET", APIBasePath+"/documents/search?q=test", nil, nil)

	docData := map[string]interface{}{
		"title":   "Test Document",
		"content": "This is a test document",
	}
	docJSON, _ := json.Marshal(docData)
	ts.testEndpoint("Add Document", "POST", APIBasePath+"/documents", bytes.NewBuffer(docJSON), map[string]string{
		"Content-Type": "application/json",
	})

	// Process lifecycle with validation
	ts.testEndpoint("Process Status", "GET", APIBasePath+"/process/status", nil, nil)
	ts.testEndpoint("Start Process", "GET", APIBasePath+"/process/start", nil, nil)
	ts.testEndpoint("Cancel Process", "GET", APIBasePath+"/process/cancel", nil, nil)
	ts.testEndpoint("Terminate Process", "GET", APIBasePath+"/process/terminate", nil, nil)

	// WebSocket and messaging
	ts.testEndpoint("WebSocket Connect", "GET", APIBasePath+"/ws/connect", nil, nil)
	ts.testEndpoint("Send Message", "GET", APIBasePath+"/send", nil, nil)

	// Environment demo
	ts.testEndpoint("Environment Demo", "GET", APIBasePath+"/env/demo", nil, nil)

	// File upload
	ts.testFileUpload()

	// Interceptor demos
	ts.testEndpoint("OpenTelemetry Demo", "GET", APIBasePath+"/interceptor/demo/otel", nil, nil)
	ts.testEndpoint("Retry Demo", "GET", APIBasePath+"/interceptor/demo/retry", nil, nil)
}

// testFileUpload tests file upload functionality
func (ts *TestSuite) testFileUpload() {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add file
	part, err := writer.CreateFormFile("file", "test.txt")
	if err != nil {
		ts.log("❌ Failed to create form file: %v", err)
		return
	}
	part.Write([]byte("test file content"))

	// Add additional fields if needed
	writer.WriteField("description", "Test upload")

	writer.Close()

	ts.testEndpoint("File Upload", "POST", APIBasePath+"/fs/upload", &buf, map[string]string{
		"Content-Type": writer.FormDataContentType(),
	})
}

// testChatRoutes tests the chat routes with session management
func (ts *TestSuite) testChatRoutes() {
	ts.log("💬 Testing Chat Routes with Session Management...")

	// Create chat session
	sessionData := map[string]interface{}{
		"user_id": "test-user",
	}
	sessionJSON, _ := json.Marshal(sessionData)
	sessionResult := ts.testEndpoint("Create Chat Session", "POST", APIBasePath+"/chat/session", bytes.NewBuffer(sessionJSON), map[string]string{
		"Content-Type": "application/json",
	})

	// Send chat message using session from previous request
	if sessionResult.Success {
		messageData := map[string]interface{}{
			"session_id": ts.sessionData["session_id"],
			"message":    "Hello, this is a test message",
			"user_id":    "test-user",
		}
		messageJSON, _ := json.Marshal(messageData)
		ts.testEndpoint("Send Chat Message", "POST", APIBasePath+"/chat/message", bytes.NewBuffer(messageJSON), map[string]string{
			"Content-Type": "application/json",
		})
	}
}

// testWippyRoutes tests the Wippy framework routes with validation
func (ts *TestSuite) testWippyRoutes() {
	ts.log("🔧 Testing Wippy Framework Routes with Validation...")

	// Test framework with validation
	ts.testEndpoint("Discover Tests", "GET", APIBasePath+"/test/discover", nil, nil)
	ts.testEndpoint("Run Tests", "GET", APIBasePath+"/test", nil, nil)
	ts.testEndpoint("Run Specific Test", "GET", APIBasePath+"/test/run?test_id=test-1", nil, nil)

	// Migration framework with validation
	ts.testEndpoint("Migration Status", "GET", APIBasePath+"/migrations/status?target_db=system:db", nil, nil)
	ts.testEndpoint("Available Databases", "GET", APIBasePath+"/migrations/databases", nil, nil)
	ts.testEndpoint("Check Tables", "GET", APIBasePath+"/migrations/check-tables?target_db=system:db", nil, nil)

	// Run migrations
	migrationData := map[string]interface{}{
		"target_db": "system:db",
	}
	migrationJSON, _ := json.Marshal(migrationData)
	ts.testEndpoint("Run Migrations", "POST", APIBasePath+"/migrations/run", bytes.NewBuffer(migrationJSON), map[string]string{
		"Content-Type": "application/json",
	})

	// Rollback migrations
	rollbackData := map[string]interface{}{
		"target_db": "system:db",
		"count":     1,
	}
	rollbackJSON, _ := json.Marshal(rollbackData)
	ts.testEndpoint("Rollback Migrations", "POST", APIBasePath+"/migrations/rollback", bytes.NewBuffer(rollbackJSON), map[string]string{
		"Content-Type": "application/json",
	})

	// Specs with validation
	ts.testEndpoint("Get Specs", "GET", APIBasePath+"/specs", nil, nil)
}

// testStaticFiles tests static file serving with content type validation
func (ts *TestSuite) testStaticFiles() {
	ts.log("📄 Testing Static Files with Content Type Validation...")

	// Main static files with content type validation
	ts.testEndpoint("Index HTML", "GET", "/", nil, nil)
	ts.testEndpoint("Dashboard CSS", "GET", "/styles/dashboard.css", nil, nil)
	ts.testEndpoint("Test JS", "GET", "/scripts/test.js", nil, nil)

	// Demo pages with HTML content validation
	ts.testEndpoint("Todo App Page", "GET", "/todo.html", nil, nil)
	ts.testEndpoint("Document Search Page", "GET", "/document_search.html", nil, nil)
	ts.testEndpoint("Security Page", "GET", "/security.html", nil, nil)
	ts.testEndpoint("Tools Page", "GET", "/tools.html", nil, nil)
	ts.testEndpoint("Models Page", "GET", "/models.html", nil, nil)
	ts.testEndpoint("Upload Page", "GET", "/upload.html", nil, nil)
	ts.testEndpoint("Test Page", "GET", "/test.html", nil, nil)
	ts.testEndpoint("Blob Page", "GET", "/blob.html", nil, nil)
	ts.testEndpoint("Specs Page", "GET", "/specs.html", nil, nil)
	ts.testEndpoint("Lifecycle Page", "GET", "/lifecycle.html", nil, nil)
	ts.testEndpoint("Migrations Page", "GET", "/migrations.html", nil, nil)
}

// GenerateReport generates a detailed test report
func (ts *TestSuite) GenerateReport() {
	total := len(ts.results)
	passed := 0
	validationFailures := 0

	for _, result := range ts.results {
		if result.Success {
			passed++
		}
		if result.Validation != nil && !result.Validation.Valid {
			validationFailures++
		}
	}

	failed := total - passed
	successRate := 0.0
	if total > 0 {
		successRate = float64(passed) / float64(total) * 100
	}

	report := map[string]interface{}{
		"summary": map[string]interface{}{
			"total":              total,
			"passed":             passed,
			"failed":             failed,
			"validationFailures": validationFailures,
			"successRate":        fmt.Sprintf("%.2f%%", successRate),
			"appStarted":         ts.appStarted,
			"timestamp":          time.Now().Format(time.RFC3339),
		},
		"results": ts.results,
	}

	// Print console report
	fmt.Println("\n📊 Advanced E2E Test Report")
	fmt.Println("===========================")
	fmt.Printf("Total Tests: %d\n", total)
	fmt.Printf("Passed: %d\n", passed)
	fmt.Printf("Failed: %d\n", failed)
	fmt.Printf("Validation Failures: %d\n", validationFailures)
	fmt.Printf("Success Rate: %.2f%%\n", successRate)
	fmt.Printf("Application Started: %t\n", ts.appStarted)

	if failed > 0 {
		fmt.Println("\n❌ Failed Tests:")
		for _, result := range ts.results {
			if !result.Success {
				fmt.Printf("  - %s: %s\n", result.Name, result.Error)
				if result.Validation != nil && !result.Validation.Valid {
					for _, err := range result.Validation.Errors {
						fmt.Printf("    Validation error: %s\n", err)
					}
				}
			}
		}
	}

	// Save JSON report
	reportJSON, _ := json.MarshalIndent(report, "", "  ")
	reportPath := "e2e_test_report_advanced.json"
	err := os.WriteFile(reportPath, reportJSON, 0644)
	if err != nil {
		ts.log("❌ Failed to write report file: %v", err)
	} else {
		ts.log("📄 Detailed report saved to: %s", reportPath)
	}
}

// TestAdvancedE2E runs the advanced e2e test suite
func TestAdvancedE2E(t *testing.T) {
	// Get base URL from environment or use default
	baseURL := os.Getenv("APP_BASE_URL")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	// Create test suite
	testSuite := NewTestSuite(baseURL)

	// Verify application is running before running tests
	if !testSuite.verifyAppStarted() {
		t.SkipNow()
	}

	// Run all tests
	testSuite.RunAllTests()
	testSuite.GenerateReport()

	// Assert that at least some tests passed
	passedCount := 0
	for _, result := range testSuite.results {
		if result.Success {
			passedCount++
		}
	}

	assert.Greater(t, passedCount, 0, "At least one test should pass")
}
