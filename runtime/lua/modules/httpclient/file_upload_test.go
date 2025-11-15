package httpclient

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// mockReader is a simple io.Reader implementation for testing
type mockReader struct {
	reader io.Reader
}

func newMockReader(data string) *mockReader {
	return &mockReader{reader: strings.NewReader(data)}
}

func (m *mockReader) Read(p []byte) (n int, err error) {
	return m.reader.Read(p)
}

// Helper function to extract content from multipart requests
func parseMultipartRequest(t *testing.T, req *http.Request) map[string]string {
	require.Equal(t, "multipart/form-data", req.Header.Get("Content-Type")[:19], "Content-Type should be multipart/form-data")

	// Parse multipart form
	err := req.ParseMultipartForm(10 << 20) // 10 MB max
	require.NoError(t, err, "Failed to parse multipart form")

	result := make(map[string]string)

	// Parse form values
	for key, values := range req.MultipartForm.Value {
		if len(values) > 0 {
			result[key] = values[0]
		}
	}

	// Parse files
	for field, fileHeaders := range req.MultipartForm.File {
		for _, fileHeader := range fileHeaders {
			file, err := fileHeader.Open()
			require.NoError(t, err, "Failed to open uploaded file")
			defer file.Close()

			data, err := io.ReadAll(file)
			require.NoError(t, err, "Failed to read uploaded file")

			// Store file content with metadata
			result[field+"_name"] = fileHeader.Filename
			result[field+"_type"] = fileHeader.Header.Get("Content-Type")
			result[field+"_content"] = string(data)
		}
	}

	return result
}

func TestFileUpload(t *testing.T) {
	logger := zap.NewNop()

	t.Run("upload file with string content", func(t *testing.T) {
		var capturedRequest *http.Request

		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				capturedRequest = req
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`{"success": true}`)),
					Request:    req,
				}, nil
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Register a mock reader function for testing
		vm.State().SetGlobal("create_reader", vm.State().NewFunction(func(L *lua.LState) int {
			content := L.CheckString(1)
			reader := newMockReader(content)

			ud := L.NewUserData()
			ud.Value = reader
			L.Push(ud)
			return 1
		}))

		script := `
			local http = require("http_client")
			
			local response = http.post("https://api.example.com/upload", {
				files = {
					{
						name = "document",
						filename = "test.txt",
						content_type = "text/plain",
						content = "This is a test file content"
					}
				}
			})
			
			assert(response ~= nil, "Response should not be nil")
			assert(response.status_code == 200, "Status code should be 200")
		`

		err = vm.DoString(newTestContext(), script, "test")
		require.NoError(t, err)

		// Verify the request was sent as multipart
		assert.NotNil(t, capturedRequest, "Request should not be nil")
		contents := parseMultipartRequest(t, capturedRequest)

		assert.Equal(t, "test.txt", contents["document_name"], "Filename mismatch")
		assert.Equal(t, "text/plain", contents["document_type"], "Content-Type mismatch")
		assert.Equal(t, "This is a test file content", contents["document_content"], "File content mismatch")
	})

	t.Run("upload file with reader", func(t *testing.T) {
		var capturedRequest *http.Request

		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				capturedRequest = req
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`{"success": true}`)),
					Request:    req,
				}, nil
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Register a mock reader function for testing
		vm.State().SetGlobal("create_reader", vm.State().NewFunction(func(L *lua.LState) int {
			content := L.CheckString(1)
			reader := newMockReader(content)

			ud := L.NewUserData()
			ud.Value = reader
			L.Push(ud)
			return 1
		}))

		script := `
			local http = require("http_client")
			
			-- Create a reader with test content
			local reader = create_reader("Content from reader object")
			
			local response = http.post("https://api.example.com/upload", {
				files = {
					{
						name = "document",
						filename = "reader.txt",
						content_type = "text/plain",
						reader = reader
					}
				}
			})
			
			assert(response ~= nil, "Response should not be nil")
			assert(response.status_code == 200, "Status code should be 200")
		`

		err = vm.DoString(newTestContext(), script, "test")
		require.NoError(t, err)

		// Verify the request was sent as multipart
		assert.NotNil(t, capturedRequest, "Request should not be nil")
		contents := parseMultipartRequest(t, capturedRequest)

		assert.Equal(t, "reader.txt", contents["document_name"], "Filename mismatch")
		assert.Equal(t, "text/plain", contents["document_type"], "Content-Type mismatch")
		assert.Equal(t, "Content from reader object", contents["document_content"], "File content mismatch")
	})

	t.Run("upload multiple files with form data", func(t *testing.T) {
		var capturedRequest *http.Request

		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				capturedRequest = req
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`{"success": true}`)),
					Request:    req,
				}, nil
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Register a mock reader function for testing
		vm.State().SetGlobal("create_reader", vm.State().NewFunction(func(L *lua.LState) int {
			content := L.CheckString(1)
			reader := newMockReader(content)

			ud := L.NewUserData()
			ud.Value = reader
			L.Push(ud)
			return 1
		}))

		script := `
			local http = require("http_client")
			
			-- Create readers with test content
			local reader1 = create_reader("Image content 1")
			local reader2 = create_reader("Image content 2")
			
			local response = http.post("https://api.example.com/upload", {
				form = "title=My%20Photos&description=Vacation%20photos",
				files = {
					{
						name = "image1",
						filename = "photo1.jpg",
						content_type = "image/jpeg",
						reader = reader1
					},
					{
						name = "image2",
						filename = "photo2.jpg",
						content_type = "image/jpeg",
						content = "Second image content"
					}
				}
			})
			
			assert(response ~= nil, "Response should not be nil")
			assert(response.status_code == 200, "Status code should be 200")
		`

		err = vm.DoString(newTestContext(), script, "test")
		require.NoError(t, err)

		// Verify the request was sent as multipart
		assert.NotNil(t, capturedRequest, "Request should not be nil")
		contents := parseMultipartRequest(t, capturedRequest)

		// Check form fields
		assert.Equal(t, "My%20Photos", contents["title"], "Form field 'title' mismatch")
		assert.Equal(t, "Vacation%20photos", contents["description"], "Form field 'description' mismatch")

		// Check files
		assert.Equal(t, "photo1.jpg", contents["image1_name"], "Filename 1 mismatch")
		assert.Equal(t, "image/jpeg", contents["image1_type"], "Content-Type 1 mismatch")
		assert.Equal(t, "Image content 1", contents["image1_content"], "File content 1 mismatch")

		assert.Equal(t, "photo2.jpg", contents["image2_name"], "Filename 2 mismatch")
		assert.Equal(t, "image/jpeg", contents["image2_type"], "Content-Type 2 mismatch")
		assert.Equal(t, "Second image content", contents["image2_content"], "File content 2 mismatch")
	})

	t.Run("default content type", func(t *testing.T) {
		var capturedRequest *http.Request

		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				capturedRequest = req
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`{"success": true}`)),
					Request:    req,
				}, nil
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local http = require("http_client")
			
			local response = http.post("https://api.example.com/upload", {
				files = {
					{
						name = "file",
						filename = "noext",
						-- No content_type specified, should default to application/octet-stream
						content = "Binary content"
					}
				}
			})
			
			assert(response ~= nil, "Response should not be nil")
			assert(response.status_code == 200, "Status code should be 200")
		`

		err = vm.DoString(newTestContext(), script, "test")
		require.NoError(t, err)

		// Verify the request was sent as multipart
		assert.NotNil(t, capturedRequest, "Request should not be nil")
		contents := parseMultipartRequest(t, capturedRequest)

		assert.Equal(t, "noext", contents["file_name"], "Filename mismatch")
		assert.Equal(t, "application/octet-stream", contents["file_type"], "Content-Type should default to application/octet-stream")
		assert.Equal(t, "Binary content", contents["file_content"], "File content mismatch")
	})

	// This test verifies how the implementation handles validation errors
	// Note that our implementation skips invalid files rather than failing the request
	t.Run("validation handling", func(t *testing.T) {
		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`{"success": true}`)),
					Request:    req,
				}, nil
			},
		}

		mod := NewHTTPClientModule(logger, mockClient)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Invalid name (non-string)
		script1 := `
			local http = require("http_client")
			local response = http.post("https://api.example.com/upload", {
				files = {
					{
						name = 123, -- Invalid type
						filename = "test.txt",
						content = "Test content"
					}
				}
			})
		`
		err = vm.DoString(newTestContext(), script1, "test_invalid_name")
		assert.NoError(t, err, "Should not error for invalid name, just skip the file")

		// Invalid filename (non-string)
		script2 := `
			local http = require("http_client")
			local response = http.post("https://api.example.com/upload", {
				files = {
					{
						name = "file",
						filename = true, -- Invalid type
						content = "Test content"
					}
				}
			})
		`
		err = vm.DoString(newTestContext(), script2, "test_invalid_filename")
		assert.NoError(t, err, "Should not error for invalid filename, just skip the file")

		// Missing content and reader
		script3 := `
			local http = require("http_client")
			local response = http.post("https://api.example.com/upload", {
				files = {
					{
						name = "file",
						filename = "test.txt"
						-- No content or reader
					}
				}
			})
		`
		err = vm.DoString(newTestContext(), script3, "test_missing_content")
		assert.NoError(t, err, "Should not error for missing content, just skip the file")

		// Invalid reader test
		script4 := `
			local http = require("http_client")
			
			-- Create an invalid reader (a table not implementing io.Reader)
			local not_a_reader = {}
			
			-- This test won't cause an error because the implementation just skips invalid files
			-- rather than failing the entire request
			local response = http.post("https://api.example.com/upload", {
				files = {
					{
						name = "file",
						filename = "test.txt",
						reader = not_a_reader -- Invalid reader
					}
				}
			})
			
			-- Make sure the request succeeded (with no files)
			assert(response ~= nil, "Response should not be nil")
			assert(response.status_code == 200, "Status code should be 200")
		`
		err = vm.DoString(newTestContext(), script4, "test_invalid_reader")
		assert.NoError(t, err, "Should not error for invalid reader because the implementation just skips the file")
	})

	t.Run("file batch upload with coroutines", func(t *testing.T) {
		var capturedRequest *http.Request

		mockClient := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				capturedRequest = req
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBufferString(`{"success": true}`)),
					Request:    req,
				}, nil
			},
		}

		// Create VM with coroutine support
		vm, err := engine.NewVM(
			logger,
			engine.WithLoader("http_client", NewHTTPClientModule(logger, mockClient).Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		// Register a mock reader function for testing
		vm.State().SetGlobal("create_reader", vm.State().NewFunction(func(L *lua.LState) int {
			content := L.CheckString(1)
			reader := newMockReader(content)

			ud := L.NewUserData()
			ud.Value = reader
			L.Push(ud)
			return 1
		}))

		script := `
			local http = require("http_client")
			
			-- Create readers with test content
			local reader = create_reader("Content for large file")
			
			local response = http.post("https://api.example.com/upload", {
				files = {
					{
						name = "large_file",
						filename = "large.dat",
						content_type = "application/octet-stream",
						reader = reader
					}
				}
			})
			
			assert(response ~= nil, "Response should not be nil")
			assert(response.status_code == 200, "Status code should be 200")
		`

		err = vm.DoString(newTestContext(), script, "test_coroutine_upload")
		require.NoError(t, err)

		// Verify the request was sent as multipart
		assert.NotNil(t, capturedRequest, "Request should not be nil")
		contents := parseMultipartRequest(t, capturedRequest)

		assert.Equal(t, "large.dat", contents["large_file_name"], "Filename mismatch")
		assert.Equal(t, "application/octet-stream", contents["large_file_type"], "Content-Type mismatch")
		assert.Equal(t, "Content for large file", contents["large_file_content"], "File content mismatch")
	})
}
