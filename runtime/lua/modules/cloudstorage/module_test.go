package cloudstorage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/cloudstorage"
	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap/zaptest"
)

// testObject represents an object in our test cloud storage
type testObject struct {
	cloudstorage.ObjectMetadata
	content []byte
}

// mockCloudStorage implements a test version of the cloudstorage.Storage interface
type mockCloudStorage struct {
	objects map[string]testObject
}

// newMockCloudStorage creates a new mock storage with some test objects
func newMockCloudStorage() *mockCloudStorage {
	return &mockCloudStorage{
		objects: map[string]testObject{
			"test.txt": {
				ObjectMetadata: cloudstorage.ObjectMetadata{
					Key:         "test.txt",
					Size:        11,
					ContentType: "text/plain",
					ETag:        "etag1",
				},
				content: []byte("Hello World"),
			},
			"data.json": {
				ObjectMetadata: cloudstorage.ObjectMetadata{
					Key:         "data.json",
					Size:        20,
					ContentType: "application/json",
					ETag:        "etag2",
				},
				content: []byte(`{"test": "data"}`),
			},
		},
	}
}

// ListObjects implements the cloudstorage.Storage interface
func (m *mockCloudStorage) ListObjects(ctx context.Context, opts *cloudstorage.ListObjectsOptions) (*cloudstorage.ListObjectsResult, error) {
	result := &cloudstorage.ListObjectsResult{
		Objects:     []cloudstorage.ObjectMetadata{},
		IsTruncated: false,
	}

	count := 0
	for _, obj := range m.objects {
		// Apply prefix filter if specified
		if opts.Prefix != "" && !bytes.HasPrefix([]byte(obj.Key), []byte(opts.Prefix)) {
			continue
		}

		result.Objects = append(result.Objects, obj.ObjectMetadata)
		count++

		// Apply max keys limit if specified
		if opts.MaxKeys > 0 && count >= opts.MaxKeys {
			result.IsTruncated = true
			result.NextContinuationToken = "next-token"
			break
		}
	}

	return result, nil
}

// DownloadObject implements the cloudstorage.Storage interface
func (m *mockCloudStorage) DownloadObject(ctx context.Context, key string, w io.Writer, opts *cloudstorage.DownloadOptions) error {
	obj, exists := m.objects[key]
	if !exists {
		return errors.New("object not found")
	}

	// Simple implementation without handling Range option for now
	_, err := w.Write(obj.content)
	return err
}

// UploadObject implements the cloudstorage.Storage interface
func (m *mockCloudStorage) UploadObject(ctx context.Context, key string, content io.Reader) error {
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}

	m.objects[key] = testObject{
		ObjectMetadata: cloudstorage.ObjectMetadata{
			Key:         key,
			Size:        int64(len(data)),
			ContentType: "application/octet-stream", // Default
			ETag:        "etag-new",
		},
		content: data,
	}

	return nil
}

// DeleteObjects implements the cloudstorage.Storage interface
func (m *mockCloudStorage) DeleteObjects(ctx context.Context, keys []string) error {
	for _, key := range keys {
		delete(m.objects, key)
	}
	return nil
}

// PresignedGetURL implements the cloudstorage.Storage interface
func (m *mockCloudStorage) PresignedGetURL(ctx context.Context, key string, opts *cloudstorage.PresignedGetOptions) (string, error) {
	_, exists := m.objects[key]
	if !exists {
		return "", errors.New("object not found")
	}

	expiration := opts.Expiration
	if expiration == 0 {
		expiration = time.Hour
	}

	return "https://example.com/" + key + "?expires=" + expiration.String(), nil
}

// PresignedPutURL implements the cloudstorage.Storage interface
func (m *mockCloudStorage) PresignedPutURL(ctx context.Context, key string, opts *cloudstorage.PresignedPutOptions) (string, error) {
	expiration := opts.Expiration
	if expiration == 0 {
		expiration = time.Hour
	}

	url := "https://example.com/" + key + "?expires=" + expiration.String()
	if opts.ContentType != "" {
		url += "&contentType=" + opts.ContentType
	}
	if opts.ContentLength > 0 {
		url += "&contentLength=" + strconv.FormatInt(opts.ContentLength, 10)
	}

	return url, nil
}

// Mock resource for testing
type mockResource struct {
	resValue any
	released bool
}

func (m *mockResource) Get() (any, error) {
	return m.resValue, nil
}

func (m *mockResource) Release() error {
	m.released = true
	return nil
}

func (m *mockResource) Mode() resource.AccessMode {
	return resource.ModeNormal
}

// Mock registry for resource lookup
type mockResourceRegistry struct {
	resources map[registry.ID]resource.Resource[any]
}

func (m *mockResourceRegistry) Acquire(_ context.Context, id registry.ID, _ resource.AccessMode,
) (resource.Resource[any], error) {
	res, ok := m.resources[id]
	if !ok {
		return nil, resource.ErrResourceNotFound
	}
	return res, nil
}

func (m *mockResourceRegistry) List() ([]registry.ID, error) {
	ids := make([]registry.ID, 0, len(m.resources))
	for id := range m.resources {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *mockResourceRegistry) Exists(id registry.ID) bool {
	_, ok := m.resources[id]
	return ok
}

// setupTestEnvironment creates a test environment with CloudStorage module and mock storage
func setupTestEnvironment(t *testing.T, mockStorage cloudstorage.Storage) (*engine.CoroutineVM, *lua.LState, engine.UnitOfWork, *engine.Runner) {
	logger := zaptest.NewLogger(t)

	// Create the CloudStorage module
	module := NewModule()

	// Create a mock resource registry with our test cloud storage
	mockRegistry := &mockResourceRegistry{
		resources: map[registry.ID]resource.Resource[any]{
			registry.ParseID("test_storage"): &mockResource{resValue: mockStorage},
		},
	}

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Get the Lua state
	L := vm.State()

	// Register the CloudStorage module
	L.PreloadModule(module.Name(), module.Loader)

	// Create a runner with the coroutine layer
	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

	// Add the resource registry to the context
	ctx := resource.WithResources(context.Background(), mockRegistry)
	ctx = logs.WithLogger(ctx, logger)

	// Create a UOW for resource management
	uw, ctx := runner.InitUnitOfWork(ctx)

	// Set the context in the Lua state
	L.SetContext(ctx)

	return vm, L, uw, runner
}

// registerTestHelpers registers any helper functions needed for testing
func registerTestHelpers(L *lua.LState) {
	// Register function to create a buffer in Lua
	L.SetGlobal("create_buffer", L.NewFunction(func(L *lua.LState) int {
		buf := &bytes.Buffer{}
		ud := L.NewUserData()
		ud.Value = buf
		L.Push(ud)
		return 1
	}))
}

func TestCloudStorageModule(t *testing.T) {
	t.Run("Module_Get", func(t *testing.T) {
		mockStorage := newMockCloudStorage()
		vm, L, uw, _ := setupTestEnvironment(t, mockStorage)
		defer vm.Close()
		defer func() {
			err := uw.Close()
			assert.NoError(t, err, "Unit of work cleanup failed")
		}()

		err := L.DoString(`
			local cs = require("cloudstorage")
			local storage, err = cs.get("test_storage")
			if err then 
				error("Error getting cloudstorage: " .. err)
			end
			
			-- Check that we got a valid object
			assert(storage ~= nil, "Storage should not be nil")
		`)
		assert.NoError(t, err, "Getting cloud storage should succeed")
	})

	t.Run("ListObjects_Basic", func(t *testing.T) {
		mockStorage := newMockCloudStorage()
		vm, L, uw, _ := setupTestEnvironment(t, mockStorage)
		defer vm.Close()
		defer func() {
			err := uw.Close()
			assert.NoError(t, err, "Unit of work cleanup failed")
		}()

		err := L.DoString(`
			local cs = require("cloudstorage")
			local storage, err = cs.get("test_storage")
			if err then 
				error("Error getting cloudstorage: " .. err)
			end
			
			-- Call list_objects with no options
			local result = storage:list_objects()
			
			-- Verify result structure
			assert(result ~= nil, "Result should not be nil")
			assert(result.objects ~= nil, "Objects array should not be nil")
			assert(#result.objects >= 2, "Should have at least two objects")
			
			-- Verify object metadata
			local foundTestTxt = false
			local foundDataJson = false
			
			for i, obj in ipairs(result.objects) do
				assert(obj.key ~= nil, "Object key should not be nil")
				assert(obj.size ~= nil, "Object size should not be nil")
				assert(obj.content_type ~= nil, "Content type should not be nil")
				assert(obj.etag ~= nil, "ETag should not be nil")
				
				if obj.key == "test.txt" then
					foundTestTxt = true
					assert(obj.size == 11, "test.txt size should be 11")
					assert(obj.content_type == "text/plain", "test.txt content type should be text/plain")
				elseif obj.key == "data.json" then
					foundDataJson = true
					assert(obj.size == 20, "data.json size should be 20")
					assert(obj.content_type == "application/json", "data.json content type should be application/json")
				end
			end
			
			assert(foundTestTxt, "test.txt should be in results")
			assert(foundDataJson, "data.json should be in results")
		`)
		assert.NoError(t, err, "Basic list objects test failed")
	})

	t.Run("ListObjects_WithOptions", func(t *testing.T) {
		mockStorage := newMockCloudStorage()
		vm, L, uw, _ := setupTestEnvironment(t, mockStorage)
		defer vm.Close()
		defer func() {
			err := uw.Close()
			assert.NoError(t, err, "Unit of work cleanup failed")
		}()

		err := L.DoString(`
			local cs = require("cloudstorage")
			local storage, err = cs.get("test_storage")
			if err then 
				error("Error getting cloudstorage: " .. err)
			end
			
			-- Call list_objects with prefix option
			local result = storage:list_objects({
				prefix = "test",
				max_keys = 1
			})
			
			-- Verify filtering and pagination
			assert(result ~= nil, "Result should not be nil")
			assert(#result.objects == 1, "Should have exactly one object with prefix 'test'")
			assert(result.is_truncated == true, "Result should be truncated")
			assert(result.next_continuation_token ~= nil, "Should have continuation token")
		`)
		assert.NoError(t, err, "ListObjects with options test failed")
	})

	t.Run("DownloadObject", func(t *testing.T) {
		mockStorage := newMockCloudStorage()
		vm, L, uw, _ := setupTestEnvironment(t, mockStorage)
		defer vm.Close()
		defer func() {
			err := uw.Close()
			assert.NoError(t, err, "Unit of work cleanup failed")
		}()

		registerTestHelpers(L)

		err := L.DoString(`
			local cs = require("cloudstorage")
			local storage, err = cs.get("test_storage")
			if err then 
				error("Error getting cloudstorage: " .. err)
			end
			
			-- Create a buffer to download into
			local buf = create_buffer()
			
			-- Download test.txt
			local success = storage:download_object("test.txt", buf)
			assert(success, "Download should succeed")
			
			-- Since we can't directly access the buffer contents from Lua,
			-- we'll test upload using this buffer to verify the round trip
			success = storage:upload_object("test_copy.txt", buf)
			assert(success, "Upload should succeed")
			
			-- List objects to verify the new file exists
			local result = storage:list_objects()
			
			local found = false
			for _, obj in ipairs(result.objects) do
				if obj.key == "test_copy.txt" then
					found = true
					assert(obj.size == 11, "Size should be 11")
					break
				end
			end
			
			assert(found, "test_copy.txt should exist after upload")
		`)
		assert.NoError(t, err, "DownloadObject test failed")
	})

	t.Run("UploadObject", func(t *testing.T) {
		mockStorage := newMockCloudStorage()
		vm, L, uw, _ := setupTestEnvironment(t, mockStorage)
		defer vm.Close()
		defer func() {
			err := uw.Close()
			assert.NoError(t, err, "Unit of work cleanup failed")
		}()

		err := L.DoString(`
			local cs = require("cloudstorage")
			local storage, err = cs.get("test_storage")
			if err then 
				error("Error getting cloudstorage: " .. err)
			end
			
			-- Upload string content
			local content = "New object content"
			local success = storage:upload_object("new-file.txt", content)
			assert(success, "Upload should succeed")
			
			-- List objects to verify the new file exists
			local result = storage:list_objects()
			
			local found = false
			for _, obj in ipairs(result.objects) do
				if obj.key == "new-file.txt" then
					found = true
					assert(obj.size == #content, "Size should match content length")
					break
				end
			end
			
			assert(found, "new-file.txt should exist after upload")
		`)
		assert.NoError(t, err, "UploadObject test failed")
	})

	t.Run("DeleteObjects", func(t *testing.T) {
		mockStorage := newMockCloudStorage()
		vm, L, uw, _ := setupTestEnvironment(t, mockStorage)
		defer vm.Close()
		defer func() {
			err := uw.Close()
			assert.NoError(t, err, "Unit of work cleanup failed")
		}()

		err := L.DoString(`
			local cs = require("cloudstorage")
			local storage, err = cs.get("test_storage")
			if err then 
				error("Error getting cloudstorage: " .. err)
			end
			
			-- First verify we have the expected objects
			local result = storage:list_objects()
			assert(#result.objects >= 2, "Should have at least two objects initially")
			
			-- Delete test.txt
			local success = storage:delete_objects({"test.txt"})
			assert(success, "Delete should succeed")
			
			-- Verify test.txt is gone
			result = storage:list_objects()
			
			for _, obj in ipairs(result.objects) do
				assert(obj.key ~= "test.txt", "test.txt should be deleted")
			end
			
			-- Delete multiple objects
			success = storage:delete_objects({"data.json"})
			assert(success, "Delete should succeed")
			
			-- Verify all objects are gone
			result = storage:list_objects()
			assert(#result.objects == 0, "All objects should be deleted")
		`)
		assert.NoError(t, err, "DeleteObjects test failed")
	})

	t.Run("PresignedURLs", func(t *testing.T) {
		mockStorage := newMockCloudStorage()
		vm, L, uw, _ := setupTestEnvironment(t, mockStorage)
		defer vm.Close()
		defer func() {
			err := uw.Close()
			assert.NoError(t, err, "Unit of work cleanup failed")
		}()

		err := L.DoString(`
			local cs = require("cloudstorage")
			local storage, err = cs.get("test_storage")
			if err then 
				error("Error getting cloudstorage: " .. err)
			end
			
			-- Generate presigned GET URL
			local getUrl = storage:presigned_get_url("test.txt", {
				expiration = 3600 -- 1 hour
			})
			
			assert(getUrl ~= nil, "GET URL should not be nil")
			assert(string.find(getUrl, "https://"), "URL should be HTTPS")
			assert(string.find(getUrl, "test.txt"), "URL should contain object key")
			assert(string.find(getUrl, "expires"), "URL should contain expiration")
			
			-- Generate presigned PUT URL
			local putUrl = storage:presigned_put_url("newupload.txt", {
				expiration = 1800, -- 30 minutes
				content_type = "text/plain",
				content_length = 1024
			})

			assert(putUrl ~= nil, "PUT URL should not be nil")
			assert(string.find(putUrl, "https://"), "URL should be HTTPS")
			assert(string.find(putUrl, "newupload.txt"), "URL should contain object key")
			assert(string.find(putUrl, "expires"), "URL should contain expiration")
			assert(string.find(putUrl, "contentType"), "URL should contain content type")
		`)
		assert.NoError(t, err, "PresignedURLs test failed")
	})

	t.Run("ErrorHandling", func(t *testing.T) {
		mockStorage := newMockCloudStorage()
		vm, L, uw, _ := setupTestEnvironment(t, mockStorage)
		defer vm.Close()
		defer func() {
			err := uw.Close()
			assert.NoError(t, err, "Unit of work cleanup failed")
		}()

		// Test error when trying to get an object that doesn't exist
		err := L.DoString(`
			local cs = require("cloudstorage")
			local storage, err = cs.get("nonexistent_storage")
			assert(storage == nil, "Storage should be nil for nonexistent resource")
			assert(err ~= nil, "Error should not be nil for nonexistent resource")
			assert(string.find(err, "failed to acquire resource"), "Error should mention resource acquisition")
		`)
		assert.NoError(t, err, "ErrorHandling test failed")
	})
}
