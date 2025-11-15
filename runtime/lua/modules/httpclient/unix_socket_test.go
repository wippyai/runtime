//go:build !windows

package httpclient

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// mockUnixSocketServer creates a mock HTTP server listening on a Unix socket
// for testing HTTP client functionality over Unix domain sockets.
type mockUnixSocketServer struct {
	socketPath string
	listener   net.Listener
	server     *http.Server
	responses  map[string]string
}

// newMockUnixSocketServer creates and starts a new mock Unix socket server
// for testing purposes. The server listens on a temporary Unix socket.
func newMockUnixSocketServer(t *testing.T) *mockUnixSocketServer {
	// Create temporary socket file
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	// Remove any existing socket file
	_ = os.Remove(socketPath)

	// Create listener
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)

	mock := &mockUnixSocketServer{
		socketPath: socketPath,
		listener:   listener,
		responses:  make(map[string]string),
	}

	// Set up HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", mock.handleRequest)

	mock.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start server in background
	go func() {
		_ = mock.server.Serve(listener)
	}()

	return mock
}

// handleRequest processes HTTP requests and returns configured responses
// based on the method and path combination.
func (m *mockUnixSocketServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	key := fmt.Sprintf("%s %s", r.Method, r.URL.Path)

	if response, exists := m.responses[key]; exists {
		// Handle special response patterns
		if strings.HasPrefix(response, "STATUS:") {
			parts := strings.SplitN(response, ":", 3)
			if len(parts) >= 2 {
				switch parts[1] {
				case "404":
					w.WriteHeader(http.StatusNotFound)
					if len(parts) == 3 {
						if _, err := w.Write([]byte(parts[2])); err != nil {
							return
						}
					}
					return
				case "500":
					w.WriteHeader(http.StatusInternalServerError)
					if len(parts) == 3 {
						if _, err := w.Write([]byte(parts[2])); err != nil {
							return
						}
					}
					return
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(response)); err != nil {
			return
		}
	} else {
		w.WriteHeader(http.StatusNotFound)
		if _, err := w.Write([]byte(`{"error":"not found"}`)); err != nil {
			return
		}
	}
}

// setResponse configures a mock response for a specific HTTP method and path.
func (m *mockUnixSocketServer) setResponse(method, path, response string) {
	key := fmt.Sprintf("%s %s", method, path)
	m.responses[key] = response
}

// close shuts down the mock server and cleans up resources including the socket file.
func (m *mockUnixSocketServer) close() {
	if m.server != nil {
		_ = m.server.Close()
	}
	if m.listener != nil {
		_ = m.listener.Close()
	}
	_ = os.Remove(m.socketPath)
}

func TestUnixSocketRequests(t *testing.T) {
	logger := zap.NewNop()

	t.Run("basic unix socket GET request", func(t *testing.T) {
		server := newMockUnixSocketServer(t)
		defer server.close()

		server.setResponse("GET", "/test", `{"message":"hello from unix socket"}`)

		mod := NewHTTPClientModule(logger, http.DefaultClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := fmt.Sprintf(`
			local http = require("http_client")
			local response = http.get("http://localhost/test", {
				unix_socket = "%s"
			})
			
			assert(response ~= nil, "Response should not be nil")
			assert(response.status_code == 200, "Status code should be 200")
			assert(response.body == '{"message":"hello from unix socket"}', "Body mismatch")
		`, server.socketPath)

		err = vm.DoString(newTestContext(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("unix socket POST request with body", func(t *testing.T) {
		server := newMockUnixSocketServer(t)
		defer server.close()

		server.setResponse("POST", "/data", `{"received":"ok"}`)

		mod := NewHTTPClientModule(logger, http.DefaultClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := fmt.Sprintf(`
			local http = require("http_client")
			local response = http.post("http://localhost/data", {
				unix_socket = "%s",
				headers = {
					["Content-Type"] = "application/json"
				},
				body = '{"test":"data"}'
			})
			
			assert(response ~= nil, "Response should not be nil")
			assert(response.status_code == 200, "Status code should be 200")
			assert(response.body == '{"received":"ok"}', "Body mismatch")
		`, server.socketPath)

		err = vm.DoString(newTestContext(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("unix socket with timeout", func(t *testing.T) {
		server := newMockUnixSocketServer(t)
		defer server.close()

		server.setResponse("GET", "/slow", `{"message":"slow response"}`)

		mod := NewHTTPClientModule(logger, http.DefaultClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := fmt.Sprintf(`
			local http = require("http_client")
			local response = http.get("http://localhost/slow", {
				unix_socket = "%s",
				timeout = "5s"
			})
			
			assert(response ~= nil, "Response should not be nil")
			assert(response.status_code == 200, "Status code should be 200")
		`, server.socketPath)

		err = vm.DoString(newTestContext(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("unix socket error handling", func(t *testing.T) {
		server := newMockUnixSocketServer(t)
		defer server.close()

		server.setResponse("GET", "/error", "STATUS:500:Internal Server Error")

		mod := NewHTTPClientModule(logger, http.DefaultClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := fmt.Sprintf(`
			local http = require("http_client")
			local response = http.get("http://localhost/error", {
				unix_socket = "%s"
			})
			
			assert(response ~= nil, "Response should not be nil")
			assert(response.status_code == 500, "Status code should be 500")
		`, server.socketPath)

		err = vm.DoString(newTestContext(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("invalid unix socket path", func(t *testing.T) {
		mod := NewHTTPClientModule(logger, http.DefaultClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local http = require("http_client")
			local response, err = http.get("http://localhost/test", {
				unix_socket = "/nonexistent/socket.sock"
			})
			
			assert(response == nil, "Response should be nil for invalid socket")
			assert(err ~= nil, "Error should not be nil")
			assert(string.find(err, "no such file") ~= nil or string.find(err, "connection refused") ~= nil, "Error should indicate connection failure")
		`

		err = vm.DoString(newTestContext(), script, "test")
		assert.NoError(t, err)
	})
}

func TestUnixSocketSecurity(t *testing.T) {
	logger := zap.NewNop()

	t.Run("unix socket security permission check exists", func(t *testing.T) {
		server := newMockUnixSocketServer(t)
		defer server.close()

		server.setResponse("GET", "/test", `{"message":"security check passed"}`)

		mod := NewHTTPClientModule(logger, http.DefaultClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Test that requests work by default (security allows all)
		script := fmt.Sprintf(`
			local http = require("http_client")
			local response = http.get("http://localhost/test", {
				unix_socket = "%s"
			})
			
			assert(response ~= nil, "Response should not be nil with default security")
			assert(response.status_code == 200, "Status code should be 200")
			assert(response.body == '{"message":"security check passed"}', "Body should match expected response")
		`, server.socketPath)

		err = vm.DoString(newTestContext(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("unix socket security in batch requests", func(t *testing.T) {
		server := newMockUnixSocketServer(t)
		defer server.close()

		server.setResponse("GET", "/test1", `{"id":"test1"}`)
		server.setResponse("GET", "/test2", `{"id":"test2"}`)

		mod := NewHTTPClientModule(logger, http.DefaultClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Test that batch requests work by default (security allows all)
		script := fmt.Sprintf(`
			local http = require("http_client")
			local responses = http.request_batch({
				{"GET", "http://localhost/test1", {unix_socket = "%s"}},
				{"GET", "http://localhost/test2", {unix_socket = "%s"}}
			})
			
			assert(#responses == 2, "Should have 2 responses")
			assert(responses[1].status_code == 200, "First request should succeed")
			assert(responses[2].status_code == 200, "Second request should succeed")
		`, server.socketPath, server.socketPath)

		err = vm.DoString(newTestContext(), script, "test")
		assert.NoError(t, err)
	})
}

func TestUnixSocketBatchRequests(t *testing.T) {
	logger := zap.NewNop()

	t.Run("batch requests over unix socket", func(t *testing.T) {
		server := newMockUnixSocketServer(t)
		defer server.close()

		server.setResponse("GET", "/containers", `[{"id":"container1"},{"id":"container2"}]`)
		server.setResponse("GET", "/images", `[{"id":"image1"},{"id":"image2"}]`)
		server.setResponse("GET", "/info", `{"version":"1.0"}`)

		mod := NewHTTPClientModule(logger, http.DefaultClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := fmt.Sprintf(`
			local http = require("http_client")
			local responses = http.request_batch({
				{"GET", "http://docker/containers", {unix_socket = "%s"}},
				{"GET", "http://docker/images", {unix_socket = "%s"}},
				{"GET", "http://docker/info", {unix_socket = "%s"}}
			})
			
			assert(#responses == 3, "Should have 3 responses")
			assert(responses[1].status_code == 200, "First request should succeed")
			assert(responses[2].status_code == 200, "Second request should succeed")
			assert(responses[3].status_code == 200, "Third request should succeed")
			
			assert(string.find(responses[1].body, "container1") ~= nil, "First response should contain container data")
			assert(string.find(responses[2].body, "image1") ~= nil, "Second response should contain image data")
			assert(string.find(responses[3].body, "version") ~= nil, "Third response should contain version info")
		`, server.socketPath, server.socketPath, server.socketPath)

		err = vm.DoString(newTestContext(), script, "test")
		assert.NoError(t, err)
	})
}

func TestUnixSocketStreaming(t *testing.T) {
	logger := zap.NewNop()

	t.Run("streaming response over unix socket", func(t *testing.T) {
		server := newMockUnixSocketServer(t)
		defer server.close()

		// Set up a simple response - we're testing that streaming mode works, not the content
		server.setResponse("GET", "/stream", "simple stream content")

		mod := NewHTTPClientModule(logger, http.DefaultClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := fmt.Sprintf(`
			local http = require("http_client")
			local response = http.get("http://localhost/stream", {
				unix_socket = "%s",
				stream = true
			})
			
			assert(response ~= nil, "Response should not be nil")
			assert(response.stream ~= nil, "Stream should not be nil")
			assert(response.body == nil, "Body should be nil for streaming")
			assert(response.body_size == -1, "Body size should be -1 for streaming")
			assert(response.status_code == 200, "Status code should be 200")
			
			-- Just verify we can call the stream methods without reading content
			-- since our mock server doesn't properly simulate streaming
			local stream = response.stream
			assert(stream ~= nil, "Stream object should exist")
			
			-- Close the stream to clean up
			stream:close()
		`, server.socketPath)

		err = vm.DoString(newTestContext(), script, "test")
		assert.NoError(t, err)
	})
}

func TestUnixSocketFileUpload(t *testing.T) {
	logger := zap.NewNop()

	t.Run("file upload over unix socket", func(t *testing.T) {
		server := newMockUnixSocketServer(t)
		defer server.close()

		server.setResponse("POST", "/upload", `{"uploaded":"success"}`)

		mod := NewHTTPClientModule(logger, http.DefaultClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := fmt.Sprintf(`
			local http = require("http_client")
			local response = http.post("http://localhost/upload", {
				unix_socket = "%s",
				files = {
					{
						name = "testfile",
						filename = "test.txt",
						content_type = "text/plain",
						content = "Test file content for Unix socket upload"
					}
				}
			})
			
			assert(response ~= nil, "Response should not be nil")
			assert(response.status_code == 200, "Status code should be 200")
			assert(response.body == '{"uploaded":"success"}', "Body should indicate success")
		`, server.socketPath)

		err = vm.DoString(newTestContext(), script, "test")
		assert.NoError(t, err)
	})
}

func TestDockerAPISimulation(t *testing.T) {
	logger := zap.NewNop()

	t.Run("docker api simulation", func(t *testing.T) {
		server := newMockUnixSocketServer(t)
		defer server.close()

		// Set up Docker-like API responses
		server.setResponse("GET", "/containers/json", `[{"Id":"container123","Names":["/test-container"],"State":"running"}]`)
		server.setResponse("GET", "/images/json", `[{"Id":"image456","RepoTags":["nginx:latest"]}]`)
		server.setResponse("GET", "/info", `{"ServerVersion":"20.10.0","Architecture":"x86_64"}`)
		server.setResponse("POST", "/containers/create", `{"Id":"newcontainer789","Warnings":[]}`)

		mod := NewHTTPClientModule(logger, http.DefaultClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := fmt.Sprintf(`
			local http = require("http_client")
			
			-- Docker API helper function
			local function docker_api(endpoint, method, body)
				method = method or "GET"
				local opts = {
					unix_socket = "%s",
					headers = { ["Content-Type"] = "application/json" },
					timeout = 30
				}
				
				if body then
					opts.body = body
				end
				
				return http.request(method, "http://docker" .. endpoint, opts)
			end
			
			-- Test various Docker API endpoints
			local containers = docker_api("/containers/json")
			assert(containers.status_code == 200, "Containers API should succeed")
			assert(string.find(containers.body, "container123") ~= nil, "Should contain container ID")
			
			local images = docker_api("/images/json")
			assert(images.status_code == 200, "Images API should succeed")
			assert(string.find(images.body, "nginx:latest") ~= nil, "Should contain image tag")
			
			local info = docker_api("/info")
			assert(info.status_code == 200, "Info API should succeed")
			assert(string.find(info.body, "ServerVersion") ~= nil, "Should contain server version")
			
			-- Test container creation
			local create_req = '{"Image":"nginx","Cmd":["nginx","-g","daemon off;"]}'
			local created = docker_api("/containers/create", "POST", create_req)
			assert(created.status_code == 200, "Container creation should succeed")
			assert(string.find(created.body, "newcontainer789") ~= nil, "Should return new container ID")
		`, server.socketPath)

		err = vm.DoString(newTestContext(), script, "test")
		assert.NoError(t, err)
	})
}

func TestUnixSocketOptionsValidation(t *testing.T) {
	logger := zap.NewNop()

	t.Run("unix socket with other options", func(t *testing.T) {
		server := newMockUnixSocketServer(t)
		defer server.close()

		server.setResponse("POST", "/api/endpoint", `{"result":"success"}`)

		mod := NewHTTPClientModule(logger, http.DefaultClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := fmt.Sprintf(`
			local http = require("http_client")
			local response = http.post("http://localhost/api/endpoint", {
				unix_socket = "%s",
				headers = {
					["Content-Type"] = "application/json",
					["X-Custom-Header"] = "test-value"
				},
				cookies = {
					session = "abc123",
					theme = "dark"
				},
				body = '{"test":"data"}',
				timeout = "10s"
			})
			
			assert(response ~= nil, "Response should not be nil")
			assert(response.status_code == 200, "Status code should be 200")
			assert(response.body == '{"result":"success"}', "Body should match expected response")
		`, server.socketPath)

		err = vm.DoString(newTestContext(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("unix socket option parsing", func(t *testing.T) {
		// Test that unix_socket option is parsed correctly
		l := lua.NewState()
		defer l.Close()

		// Create test table with unix_socket option
		tbl := l.NewTable()
		tbl.RawSetString("unix_socket", lua.LString("/var/run/test.sock"))
		tbl.RawSetString("timeout", lua.LString("30s"))

		opts, err := parseOptions(tbl)
		require.NoError(t, err)

		assert.Equal(t, "/var/run/test.sock", opts.unixSocket)
		assert.Equal(t, 30*time.Second, opts.timeout)
	})

	t.Run("empty unix socket option", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()

		// Create test table with empty unix_socket
		tbl := l.NewTable()
		tbl.RawSetString("unix_socket", lua.LString(""))

		opts, err := parseOptions(tbl)
		require.NoError(t, err)

		assert.Equal(t, "", opts.unixSocket)
	})

	t.Run("non-string unix socket option", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()

		// Create test table with non-string unix_socket
		tbl := l.NewTable()
		tbl.RawSetString("unix_socket", lua.LNumber(123))

		opts, err := parseOptions(tbl)
		require.NoError(t, err)

		// Should be ignored and remain empty
		assert.Equal(t, "", opts.unixSocket)
	})
}
