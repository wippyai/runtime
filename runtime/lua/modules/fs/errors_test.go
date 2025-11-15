package fs

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	fsapi "github.com/wippyai/runtime/api/fs"
)

func TestFSErrorMetadata(t *testing.T) {
	t.Run("read error has metadata", func(t *testing.T) {
		mockFS := newMockFS()
		mockRes := &mockResource{resValue: mockFS}
		vm, runner, ctx := setupLuaWithFS(t, mockRes)
		defer vm.Close()

		fsRegistry := &mockFSRegistry{
			filesystems: map[string]fsapi.FS{
				"test_fs": mockFS,
			},
		}
		ctx = fsapi.WithRegistry(ctx, fsRegistry)

		err := vm.Import(`
			function test_read_error()
				local fs = require("fs")
				local m, err = fs.get("test_fs")
				if err then error("Failed to get fs: " .. tostring(err)) end

				local file, err = m:open("/nonexistent", "r")
				assert(file == nil, "expected nil file")
				assert(err ~= nil, "expected error")
				assert(tostring(err):find("no such file") ~= nil, "expected file not found error")
			end
		`, "test", "test_read_error")
		require.NoError(t, err)

		_, err = runner.Execute(ctx, "test_read_error")
		assert.NoError(t, err)
	})

	t.Run("IO error has metadata", func(t *testing.T) {
		mockFS := newMockFS()
		mockFile := newMockFile("test.txt", "content")
		mockFile.closed = true
		mockFS.files["test.txt"] = mockFile

		mockRes := &mockResource{resValue: mockFS}
		vm, runner, ctx := setupLuaWithFS(t, mockRes)
		defer vm.Close()

		fsRegistry := &mockFSRegistry{
			filesystems: map[string]fsapi.FS{
				"test_fs": mockFS,
			},
		}
		ctx = fsapi.WithRegistry(ctx, fsRegistry)

		err := vm.Import(`
			function test_io_error()
				local fs = require("fs")
				local m, err = fs.get("test_fs")
				if err then error("Failed to get fs: " .. tostring(err)) end

				local file, err = m:open("test.txt", "r")
				assert(file ~= nil, "expected file")

				local ok, close_err = file:close()
				assert(ok == true, "expected successful close")

				local content, read_err = file:read(1024)
				assert(content == nil, "expected nil content after close")
				assert(read_err ~= nil, "expected error after close")

				assert(read_err:kind() == "Internal", "expected Internal kind, got: " .. read_err:kind())
				assert(read_err:retryable() == false, "expected non-retryable")

				local details = read_err:details()
				assert(details ~= nil, "expected details")
				assert(details.operation == "read", "expected operation in details, got: " .. tostring(details.operation))
			end
		`, "test", "test_io_error")
		require.NoError(t, err)

		_, err = runner.Execute(ctx, "test_io_error")
		assert.NoError(t, err)
	})

	t.Run("error metadata backward compatible", func(t *testing.T) {
		mockFS := newMockFS()
		mockFS.AddError("/nonexistent", fs.ErrNotExist)

		mockRes := &mockResource{resValue: mockFS}
		vm, runner, ctx := setupLuaWithFS(t, mockRes)
		defer vm.Close()

		fsRegistry := &mockFSRegistry{
			filesystems: map[string]fsapi.FS{
				"test_fs": mockFS,
			},
		}
		ctx = fsapi.WithRegistry(ctx, fsRegistry)

		err := vm.Import(`
			function test_backward_compat()
				local fs = require("fs")
				local m, err = fs.get("test_fs")
				if err then error("Failed to get fs: " .. tostring(err)) end

				local file, err = m:open("/nonexistent", "r")
				assert(file == nil, "expected nil file")
				assert(err ~= nil, "expected error")
				assert(tostring(err):find("no such file") ~= nil, "expected error message")
			end
		`, "test", "test_backward_compat")
		require.NoError(t, err)

		_, err = runner.Execute(ctx, "test_backward_compat")
		assert.NoError(t, err)
	})
}
