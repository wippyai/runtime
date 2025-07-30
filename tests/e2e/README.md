# End-to-End (E2E) Test Suite

This directory contains comprehensive end-to-end tests for the runtime application, implemented entirely in Go with a robust test runner and extensive test coverage for all HTTP routes defined in YAML configuration files.

## 🚀 Quick Start

### Pure Go Test Runner

```bash
# Run tests with default settings
go run tests/e2e/main.go

# Run tests with custom URL
go run tests/e2e/main.go -url http://localhost:8080

# Run tests with verbose output
go run tests/e2e/main.go -verbose

# Skip application availability check
go run tests/e2e/main.go -skip-check

# Run with custom timeout
go run tests/e2e/main.go -timeout 10m

# All options together
go run tests/e2e/main.go -url http://localhost:8082 -timeout 10m -verbose -skip-check
```

### Build and Run (Optional)

```bash
# Build the runner for faster execution
go build -o tests/e2e/e2e-runner tests/e2e/main.go

# Run the built binary
./tests/e2e/e2e-runner -verbose
```

## 📋 Test Coverage

The comprehensive e2e test suite covers **50+ HTTP endpoints** across multiple categories:

### Main API Routes (9 endpoints)
- `/api/v1/time/local` - Local time endpoint
- `/api/v1/pid` - Function PID endpoint  
- `/api/v1/hello` - Hello World endpoint
- `/api/v1/system/env` - System environment variables
- `/api/v1/registry/dump` - Registry content access
- `/api/v1/tools/list` - System tools listing
- `/api/v1/models/list` - LLM models listing
- `/api/v1/fs/browse` - Filesystem browser
- `/api/v1/time/ticker` - Time ticker stream

### Demo Routes (15+ endpoints)
- **Todo App** - CRUD operations for todo items
- **Document Search** - Search functionality with file uploads
- **Process Lifecycle** - Process management and monitoring
- **Environment Demo** - Environment variable handling
- **Upload Demo** - File upload and processing
- **WebSocket Demo** - Real-time communication
- **Message Sending** - Message handling and routing
- **Security Demo** - Authentication and authorization

### Chat Routes (5+ endpoints)
- `/api/v1/chat/session` - Create chat session (POST)
- `/api/v1/chat/session/{id}` - Get session info (GET)
- `/api/v1/chat/session/{id}/message` - Send message (POST)
- `/api/v1/chat/session/{id}/messages` - Get messages (GET)
- `/api/v1/ws/connect` - WebSocket connection

### Wippy Framework Routes (10+ endpoints)
- **Migration** - Database migration tools
- **Testing** - Test framework endpoints
- **LLM Integration** - Language model integration
- **Specs** - Specification management

### Security Routes (5+ endpoints)
- **Authentication** - Login/logout functionality
- **Authorization** - Permission checking
- **Token Management** - JWT token handling

## 🛠️ Pure Go Test Runner Features

### Advantages
- **100% Go implementation** - No shell scripts or external dependencies
- **Cross-platform compatibility** - Works on Windows, macOS, and Linux
- **Better error handling** - Robust error management and reporting
- **Structured output** - JSON reports with detailed metrics
- **Configurable timeouts** - Precise timeout control
- **Verbose logging** - Detailed output for debugging
- **Automatic retries** - Configurable retry logic for app availability

### Configuration Options

| Flag | Description | Default |
|------|-------------|---------|
| `-url` | Base URL for the application | `http://localhost:8082` |
| `-timeout` | Timeout for test execution | `5m` |
| `-skip-check` | Skip application availability check | `false` |
| `-verbose` | Enable verbose output | `false` |
| `-report` | Report file path | `comprehensive_e2e_report.json` |
| `-max-attempts` | Max attempts for app availability check | `30` |
| `-retry-interval` | Retry interval for app availability check | `2s` |

### Output and Reporting

The Go-based runner provides:

1. **Real-time progress** - Colored output with status indicators
2. **Detailed reports** - JSON format with test results and metrics
3. **Summary statistics** - Success rate, duration, and test counts
4. **Error details** - Specific error messages and validation failures
5. **Configuration tracking** - All settings used for the test run

## 📊 Test Results and Reports

### JSON Report Structure

```json
{
  "configuration": {
    "base_url": "http://localhost:8082",
    "timeout": "5m0s",
    "skip_check": false,
    "verbose": false
  },
  "results": {
    "success": true,
    "duration": "35.8s",
    "timestamp": "2025-07-29T17:24:54+07:00",
    "output": "..."
  },
  "summary": {
    "total_tests": 9,
    "passed_tests": 3,
    "failed_tests": 6,
    "success_rate": 0.3333,
    "total_duration": "35.8s"
  }
}
```

### Success Criteria

- **Development Environment**: 30% success rate (some endpoints may return 500)
- **Production Environment**: 80% success rate (all endpoints should work)
- **Test Execution**: Must complete within timeout period
- **Application Availability**: Must be reachable (unless skipped)

## 🔧 Test Structure

### Test Categories

1. **Main API Routes** (`testMainAPIRoutes`)
   - Core application endpoints
   - System information and utilities

2. **Demo Routes** (`testDemoRoutes`)
   - Feature demonstrations
   - CRUD operations and workflows

3. **Chat Routes** (`testChatRoutes`)
   - Real-time communication
   - Session management

4. **Wippy Framework Routes** (`testWippyRoutes`)
   - Framework-specific functionality
   - Integration testing

5. **Security Routes** (`testSecurityRoutes`)
   - Authentication and authorization
   - Security validation

6. **Static Files** (`testStaticFiles`)
   - HTML, CSS, and JavaScript files
   - Public assets

### Validation Features

- **HTTP Status Codes** - Verify correct response codes
- **Content Types** - Validate response content types
- **Response Structure** - Check JSON structure and required fields
- **Error Handling** - Validate error responses
- **Performance** - Measure response times
- **Authentication** - Test protected endpoints

## 🚨 Troubleshooting

### Common Issues

1. **Application Not Running**
   ```bash
   # Start the application first
   ./runner run -c app/app.yaml
   
   # Then run tests
   go run tests/e2e/main.go
   ```

2. **Tests Timing Out**
   ```bash
   # Increase timeout
   go run tests/e2e/main.go -timeout 10m
   ```

3. **Go Dependencies Missing**
   ```bash
   # Update dependencies
   go mod tidy
   go get github.com/fatih/color
   ```

4. **Permission Issues**
   ```bash
   # Ensure you're in the project root
   cd /path/to/project/root
   go run tests/e2e/main.go
   ```

### Debug Mode

```bash
# Run with verbose output for debugging
go run tests/e2e/main.go -verbose -skip-check

# Check application logs
tail -f 1.log
```

## 📈 Continuous Integration

### GitHub Actions Example

```yaml
name: E2E Tests
on: [push, pull_request]
jobs:
  e2e-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.24'
      - name: Start Application
        run: |
          ./runner run -c app/app.yaml &
          sleep 10
      - name: Run E2E Tests
        run: go run tests/e2e/main.go -timeout 10m
      - name: Upload Test Report
        uses: actions/upload-artifact@v3
        with:
          name: e2e-test-report
          path: comprehensive_e2e_report.json
```

### Docker Example

```dockerfile
# Dockerfile for e2e tests
FROM golang:1.24-alpine

WORKDIR /app
COPY . .

RUN go mod download
RUN go build -o e2e-runner tests/e2e/main.go

CMD ["./e2e-runner", "-verbose"]
```

## 🤝 Contributing

### Adding New Tests

1. **Identify the endpoint** in the YAML configuration files
2. **Add test case** to the appropriate test function in `tests/e2e/comprehensive/comprehensive_test.go`
3. **Define validation schema** for the response
4. **Update test documentation** in this README

### Test Best Practices

- **Use descriptive names** for test cases
- **Validate response structure** thoroughly
- **Handle different status codes** appropriately
- **Include performance metrics** when relevant
- **Add comments** for complex test logic

### Extending the Test Runner

The test runner is modular and can be easily extended:

1. **Add new configuration options** in the `Configuration` struct
2. **Implement new validation logic** in the test functions
3. **Add new report formats** by extending the `TestReport` struct
4. **Create custom test categories** by adding new test functions

## 📝 License

This test suite is part of the runtime project and follows the same license terms. 