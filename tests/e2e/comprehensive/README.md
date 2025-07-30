# Comprehensive E2E Tests

This directory contains comprehensive end-to-end tests for all HTTP routes defined in the application's YAML configuration files.

## Overview

The comprehensive e2e tests cover all HTTP endpoints defined in the following YAML files:

### Main API Routes (`app/src/http/_index.yaml`)
- `/api/v1/time/local` - Local time endpoint
- `/api/v1/pid` - Function PID endpoint  
- `/api/v1/hello` - Hello World endpoint
- `/api/v1/system/env` - System environment variables
- `/api/v1/registry/dump` - Registry content access
- `/api/v1/tools/list` - System tools listing
- `/api/v1/models/list` - LLM models listing
- `/api/v1/fs/browse` - Filesystem browser
- `/api/v1/time/ticker` - Time ticker stream

### Chat Routes (`app/src/chat/http/index.yaml`)
- `/api/v1/chat/session` - Create chat session (POST)
- `/api/v1/chat/message` - Send chat message (POST)

### Demo Routes
- **Todo App** (`app/src/demos/todo_app/api/_index.yaml`)
  - `/api/v1/todos` - List todos (GET), Add todo (POST)
  - `/api/v1/todos/get` - Get specific todo (GET)
  - `/api/v1/todos/update` - Update todo (PUT)
  - `/api/v1/todos/delete` - Delete todo (DELETE)

- **Document Search** (`app/src/demos/document_search/_index.yaml`)
  - `/api/v1/documents` - List documents (GET), Add document (POST)
  - `/api/v1/documents/search` - Search documents (GET)

- **Process Lifecycle** (`app/src/demos/process_lifecycle/_index.yaml`)
  - `/api/v1/process/status` - Get process status (GET)
  - `/api/v1/process/start` - Start process (GET)
  - `/api/v1/process/cancel` - Cancel process (GET)
  - `/api/v1/process/terminate` - Terminate process (GET)

- **WebSocket Demo** (`app/src/demos/ws_demo/_index.yaml`)
  - `/api/v1/ws/connect` - WebSocket connection (GET)

- **Message Sending** (`app/src/demos/message_sending/_index.yaml`)
  - `/api/v1/send` - Send message (GET)

- **Environment Demo** (`app/src/demos/env_demo/_index.yaml`)
  - `/api/v1/env/demo` - Environment variables demo (GET)

- **File Upload** (`app/src/demos/upload/_index.yaml`)
  - `/api/v1/fs/upload` - File upload (POST)

- **Security Demo** (`app/src/demos/sec_demo/_index.yaml`)
  - `/api/v1/security` - Security API (POST)
  - `/api/v1/protected` - Protected resource (GET)
  - `/api/v1/api/secure/profile` - Secure profile (GET)
  - `/api/v1/api/secure/admin` - Secure admin (GET)

- **Interceptor Demo** (`app/src/demos/interceptor_demo/_index.yaml`)
  - `/api/v1/interceptor/demo/otel` - OpenTelemetry demo (GET)
  - `/api/v1/interceptor/demo/retry` - Retry demo (GET)
  - `/api/v1/interceptor/demo/ratelimit` - Rate limit demo (GET)
  - `/api/v1/interceptor/demo/timeout` - Timeout demo (GET)
  - `/api/v1/interceptor/demo/with_options` - With options demo (GET)

### Wippy Framework Routes
- **Test Framework** (`app/wippy/test/http/_index.yaml`)
  - `/api/v1/test/discover` - Discover tests (GET)
  - `/api/v1/test` - Run tests (GET)
  - `/api/v1/test/run` - Run specific test (GET)

- **Migration Framework** (`app/wippy/migration/http/_index.yaml`)
  - `/api/v1/migrations/status` - Migration status (GET)
  - `/api/v1/migrations/databases` - Available databases (GET)
  - `/api/v1/migrations/check-tables` - Check tables (GET)
  - `/api/v1/migrations/run` - Run migrations (POST)
  - `/api/v1/migrations/rollback` - Rollback migrations (POST)

- **Specs** (`app/wippy/specs/_index.yaml`)
  - `/api/v1/specs` - Get specs (GET)

### Static Files
- `/` - Main page
- `/document_search.html` - Document search page
- `/blob.html` - Blob page

## Features

### Comprehensive Validation
Each endpoint test includes:
- **Response Status Code Validation** - Ensures proper HTTP status codes
- **Content Type Validation** - Verifies correct content types
- **JSON Schema Validation** - Validates response structure and required fields
- **Pattern Matching** - Validates response content against expected patterns
- **Custom Validators** - Special validation for complex responses (e.g., streaming data)

### Session Management
Tests that require session data (like chat functionality) automatically:
- Create sessions when needed
- Store session IDs for subsequent requests
- Clean up session data after tests

### Error Handling
- Graceful handling of application unavailability
- Detailed error reporting with context
- Timeout handling for long-running operations

### Test Reporting
- **Real-time Progress** - Live updates during test execution
- **Detailed Results** - Individual test results with timing and validation
- **JSON Report** - Machine-readable report saved to `comprehensive_e2e_report.json`
- **Success Rate Calculation** - Overall test success percentage

## Running the Tests

### Prerequisites
- Go 1.19 or later
- Application running on the target URL (default: `http://localhost:8082`)
- `curl` command available (for health checks)

### Quick Start
```bash
# Run with default settings
./tests/e2e/run_comprehensive_tests.sh

# Run with custom URL
./tests/e2e/run_comprehensive_tests.sh -u http://localhost:8080

# Run with extended timeout
./tests/e2e/run_comprehensive_tests.sh -t 600

# Skip application availability check
./tests/e2e/run_comprehensive_tests.sh -s
```

### Manual Execution
```bash
# Set the base URL
export TEST_BASE_URL="http://localhost:8082"

# Run the tests
go test -v -timeout 300s ./tests/e2e/comprehensive/...
```

### Command Line Options
- `-u, --url URL` - Base URL for the application (default: `http://localhost:8082`)
- `-t, --timeout SEC` - Timeout in seconds (default: 300)
- `-s, --skip-check` - Skip application availability check
- `-h, --help` - Show help message

## Test Structure

### TestSuite
The main test suite provides:
- **HTTP Client Management** - Configured with timeouts and retries
- **Session Data Storage** - Maintains state between related tests
- **Validation Schema Registry** - Predefined validation rules for each endpoint
- **Result Collection** - Aggregates all test results for reporting

### Validation Schemas
Each endpoint has a predefined validation schema that includes:
- **Required Fields** - JSON fields that must be present
- **Expected Values** - Specific values that should match
- **Content Type** - Expected HTTP content type
- **Patterns** - Regex patterns for field validation
- **Custom Validators** - Special validation functions for complex cases

### Test Categories
Tests are organized into logical categories:
1. **Main API Routes** - Core system endpoints
2. **Demo Routes** - Application demo functionality
3. **Chat Routes** - Chat session management
4. **Wippy Routes** - Framework-specific endpoints
5. **Security Routes** - Authentication and authorization
6. **Interceptor Demo Routes** - Middleware functionality
7. **Static Files** - Static content serving

## Output and Reports

### Console Output
```
==========================================
  Comprehensive E2E Test Runner
==========================================

[INFO] Go version: go version go1.21.0 linux/amd64
[INFO] Checking if application is running at http://localhost:8082...
[SUCCESS] Application is running and responding
[INFO] Running comprehensive e2e tests...
[INFO] Base URL: http://localhost:8082
[INFO] Timeout: 300s
...
```

### JSON Report
The test generates a detailed JSON report (`comprehensive_e2e_report.json`) containing:
- **Summary Statistics** - Total tests, success rate, timing
- **Individual Results** - Detailed results for each endpoint test
- **Validation Details** - Specific validation errors and warnings
- **Timing Information** - Duration for each test

### Success Criteria
The test suite is considered successful if:
- At least 80% of tests pass
- All critical endpoints (main API routes) are accessible
- No critical validation errors occur

## Troubleshooting

### Common Issues

1. **Application Not Running**
   ```
   [ERROR] Application is not responding after 30 attempts
   ```
   - Ensure the application is started and running on the expected port
   - Check if the application is accessible via browser or curl

2. **Timeout Errors**
   ```
   [ERROR] Test execution failed
   ```
   - Increase the timeout using the `-t` option
   - Check if the application is under heavy load

3. **Validation Failures**
   ```
   Validation Error: Missing required field: session_id
   ```
   - Check if the application response format has changed
   - Update validation schemas if needed

4. **Network Issues**
   ```
   [ERROR] Request failed: connection refused
   ```
   - Verify the base URL is correct
   - Check firewall and network connectivity

### Debug Mode
For detailed debugging, run tests with verbose output:
```bash
go test -v -timeout 300s ./tests/e2e/comprehensive/... -test.v
```

## Contributing

### Adding New Tests
1. Identify the endpoint in the YAML configuration files
2. Add a test case to the appropriate test function
3. Define validation schema if needed
4. Update this README with the new endpoint

### Updating Validation Schemas
1. Analyze the actual response from the endpoint
2. Update the validation schema in `validationSchemas` map
3. Test the validation with real responses
4. Update documentation if needed

### Best Practices
- Keep tests independent and stateless
- Use descriptive test names
- Include proper error handling
- Validate both success and error responses
- Maintain backward compatibility 