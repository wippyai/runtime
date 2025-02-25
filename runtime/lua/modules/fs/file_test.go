package fs

import (
	"errors"
	fsapi "github.com/ponyruntime/pony/api/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

// TestFileRead tests the file:read function
func TestFileRead(t *testing.T) {
	// Create a mock filesystem with a test file
	mockFS := newMockFS()
	mockFS.AddFile("test.txt", "Hello, World!")

	// Create a mock resource with the mock filesystem
	mockRes := &mockResource{
		resValue: mockFS,
	}

	// Create a mock registry
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": mockFS,
		},
	}

	// Setup Lua with the test filesystem
	vm, L, uw, runner := setupLuaWithFS(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err)
	}()

	// Set the filesystem registry in the context
	ctx := L.Context()
	ctx = fsapi.WithContext(ctx, fsRegistry)
	L.SetContext(ctx)

	// Import test script
	err := vm.Import(`
		function test_file_read()
			local fs = require("fs")
			local fsObj, err = fs.get("test_fs")
			if err then error(err) end

			-- Open a file for reading
			local file, err = fsObj:open("test.txt", "r")
			if err then error("Failed to open file: " .. err) end

			-- Read content from the file (default size)
			local content, err = file:read()
			if err then error("Failed to read file: " .. err) end

			-- Try reading with explicit size
			local _, err = file:seek("set", 0)
			if err then error("Failed to seek: " .. err) end
			
			local sized_content, err = file:read(5)
			if err then error("Failed to read file with size: " .. err) end

			-- Try reading at EOF
			local eof_content, err = file:read()
			local is_eof = (err == "EOF")

			-- Close the file
			local ok, err = file:close()
			if err then error("Failed to close file: " .. err) end

			return {
				full_content = content,
				sized_content = sized_content,
				is_eof = is_eof
			}
		end
	`, "test", "test_file_read")
	require.NoError(t, err)

	// Execute the test function
	result, err := runner.Execute(L.Context(), "test_file_read")
	require.NoError(t, err)

	// Verify the result
	resultTable := result.(*lua.LTable)
	fullContent := resultTable.RawGetString("full_content").(lua.LString)
	sizedContent := resultTable.RawGetString("sized_content").(lua.LString)
	isEOF := resultTable.RawGetString("is_eof").(lua.LBool)

	assert.Equal(t, "Hello, World!", string(fullContent))
	assert.Equal(t, "Hello", string(sizedContent))
	assert.True(t, bool(isEOF))
}

// TestFileWrite tests the file:write function
func TestFileWrite(t *testing.T) {
	// Create a mock filesystem
	mockFS := newMockFS()

	// Create a mock resource with the mock filesystem
	mockRes := &mockResource{
		resValue: mockFS,
	}

	// Create a mock registry
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": mockFS,
		},
	}

	// Setup Lua with the test filesystem
	vm, L, uw, runner := setupLuaWithFS(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err)
	}()

	// Set the filesystem registry in the context
	ctx := L.Context()
	ctx = fsapi.WithContext(ctx, fsRegistry)
	L.SetContext(ctx)

	// Import test script
	err := vm.Import(`
		function test_file_write()
			local fs = require("fs")
			local fsObj, err = fs.get("test_fs")
			if err then error(err) end

			-- Create a new file for writing
			local file, err = fsObj:open("newfile.txt", "w")
			if err then error("Failed to create file: " .. err) end

			-- Write to the file
			local success, err = file:write("This is a test.")
			if err then error("Failed to write to file: " .. err) end

			-- Close the file
			local ok, err = file:close()
			if err then error("Failed to close file: " .. err) end

			-- Read the file back to verify content
			local readFile, err = fsObj:open("newfile.txt", "r")
			if err then error("Failed to open file for reading: " .. err) end

			local content, err = readFile:read()
			if err then error("Failed to read file: " .. err) end

			readFile:close()

			return {
				write_success = success,
				content = content
			}
		end
	`, "test", "test_file_write")
	require.NoError(t, err)

	// Execute the test function
	result, err := runner.Execute(L.Context(), "test_file_write")
	require.NoError(t, err)

	// Verify the result
	resultTable := result.(*lua.LTable)
	writeSuccess := resultTable.RawGetString("write_success").(lua.LBool)
	content := resultTable.RawGetString("content").(lua.LString)

	assert.True(t, bool(writeSuccess))
	assert.Equal(t, "This is a test.", string(content))
}

// TestFileSeek tests the file:seek function
func TestFileSeek(t *testing.T) {
	// Create a mock filesystem with a test file
	mockFS := newMockFS()
	mockFS.AddFile("seek_test.txt", "ABCDEFGHIJKLMNOPQRSTUVWXYZ")

	// Create a mock resource with the mock filesystem
	mockRes := &mockResource{
		resValue: mockFS,
	}

	// Create a mock registry
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": mockFS,
		},
	}

	// Setup Lua with the test filesystem
	vm, L, uw, runner := setupLuaWithFS(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err)
	}()

	// Set the filesystem registry in the context
	ctx := L.Context()
	ctx = fsapi.WithContext(ctx, fsRegistry)
	L.SetContext(ctx)

	// Import test script
	err := vm.Import(`
		function test_file_seek()
			local fs = require("fs")
			local fsObj, err = fs.get("test_fs")
			if err then error(err) end

			local file, err = fsObj:open("seek_test.txt", "r")
			if err then error("Failed to open file: " .. err) end

			-- Test seeking from start
			local pos, err = file:seek("set", 5)
			if err then error("Failed to seek from start: " .. err) end

			local content1, err = file:read(5)
			if err then error("Failed to read after seek: " .. err) end

			-- Test seeking from current position
			local pos2, err = file:seek("cur", 5)
			if err then error("Failed to seek from current: " .. err) end

			local content2, err = file:read(5)
			if err then error("Failed to read after seek: " .. err) end

			-- Test seeking from end
			local pos3, err = file:seek("end", -5)
			if err then error("Failed to seek from end: " .. err) end

			local content3, err = file:read(5)
			if err then error("Failed to read after seek: " .. err) end

			file:close()

			return {
				pos1 = pos,
				content1 = content1,
				pos2 = pos2,
				content2 = content2,
				pos3 = pos3,
				content3 = content3
			}
		end
	`, "test", "test_file_seek")
	require.NoError(t, err)

	// Execute the test function
	result, err := runner.Execute(L.Context(), "test_file_seek")
	require.NoError(t, err)

	// Verify the result
	resultTable := result.(*lua.LTable)
	pos1 := resultTable.RawGetString("pos1").(lua.LNumber)
	content1 := resultTable.RawGetString("content1").(lua.LString)
	pos2 := resultTable.RawGetString("pos2").(lua.LNumber)
	content2 := resultTable.RawGetString("content2").(lua.LString)
	pos3 := resultTable.RawGetString("pos3").(lua.LNumber)
	content3 := resultTable.RawGetString("content3").(lua.LString)

	assert.Equal(t, float64(5), float64(pos1))
	assert.Equal(t, "FGHIJ", string(content1))
	assert.Equal(t, float64(15), float64(pos2))
	assert.Equal(t, "PQRST", string(content2))
	assert.Equal(t, float64(21), float64(pos3))
	assert.Equal(t, "VWXYZ", string(content3))
}

// TestFileStat tests the file:stat function
func TestFileStat(t *testing.T) {
	// Create a mock filesystem with a test file
	mockFS := newMockFS()
	mockFS.AddFile("stat_test.txt", "Hello, World!")

	// Create a mock resource with the mock filesystem
	mockRes := &mockResource{
		resValue: mockFS,
	}

	// Create a mock registry
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": mockFS,
		},
	}

	// Setup Lua with the test filesystem
	vm, L, uw, runner := setupLuaWithFS(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err)
	}()

	// Set the filesystem registry in the context
	ctx := L.Context()
	ctx = fsapi.WithContext(ctx, fsRegistry)
	L.SetContext(ctx)

	// Import test script
	err := vm.Import(`
		function test_file_stat()
			local fs = require("fs")
			local fsObj, err = fs.get("test_fs")
			if err then error(err) end

			local file, err = fsObj:open("stat_test.txt", "r")
			if err then error("Failed to open file: " .. err) end

			local stat, err = file:stat()
			if err then error("Failed to stat file: " .. err) end

			file:close()

			return {
				name = stat.name,
				size = stat.size,
				is_dir = stat.is_dir,
				type = stat.type,
				mode = stat.mode
			}
		end
	`, "test", "test_file_stat")
	require.NoError(t, err)

	// Execute the test function
	result, err := runner.Execute(L.Context(), "test_file_stat")
	require.NoError(t, err)

	// Verify the result
	resultTable := result.(*lua.LTable)
	name := resultTable.RawGetString("name").(lua.LString)
	size := resultTable.RawGetString("size").(lua.LNumber)
	isDir := resultTable.RawGetString("is_dir").(lua.LBool)
	fileType := resultTable.RawGetString("type").(lua.LString)

	assert.Equal(t, "stat_test.txt", string(name))
	assert.Equal(t, float64(13), float64(size)) // "Hello, World!" = 13 bytes
	assert.False(t, bool(isDir))
	assert.Equal(t, "file", string(fileType))
}

// TestFileSync tests the file:sync function
func TestFileSync(t *testing.T) {
	// Create a mock filesystem
	mockFS := newMockFS()

	// Create a mock resource with the mock filesystem
	mockRes := &mockResource{
		resValue: mockFS,
	}

	// Create a mock registry
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": mockFS,
		},
	}

	// Setup Lua with the test filesystem
	vm, L, uw, runner := setupLuaWithFS(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err)
	}()

	// Set the filesystem registry in the context
	ctx := L.Context()
	ctx = fsapi.WithContext(ctx, fsRegistry)
	L.SetContext(ctx)

	// Import test script
	err := vm.Import(`
		function test_file_sync()
			local fs = require("fs")
			local fsObj, err = fs.get("test_fs")
			if err then error(err) end

			local file, err = fsObj:open("sync_test.txt", "w")
			if err then error("Failed to create file: " .. err) end

			local _, err = file:write("Test data")
			if err then error("Failed to write to file: " .. err) end

			local success, err = file:sync()
			if err then error("Failed to sync file: " .. err) end

			file:close()

			return {
				sync_success = success
			}
		end
	`, "test", "test_file_sync")
	require.NoError(t, err)

	// Execute the test function
	result, err := runner.Execute(L.Context(), "test_file_sync")
	require.NoError(t, err)

	// Verify the result
	resultTable := result.(*lua.LTable)
	syncSuccess := resultTable.RawGetString("sync_success").(lua.LBool)

	assert.True(t, bool(syncSuccess))
}

// TestFileErrorHandling tests error handling in file operations
func TestFileErrorHandling(t *testing.T) {
	// Create a mock filesystem
	mockFS := newMockFS()
	// Add an error case
	mockFS.AddError("error_file.txt", errors.New("simulated error"))

	// Create a mock resource with the mock filesystem
	mockRes := &mockResource{
		resValue: mockFS,
	}

	// Create a mock registry
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": mockFS,
		},
	}

	// Setup Lua with the test filesystem
	vm, L, uw, runner := setupLuaWithFS(t, mockRes)
	defer vm.Close()
	defer func() {
		err := uw.Close()
		assert.NoError(t, err)
	}()

	// Set the filesystem registry in the context
	ctx := L.Context()
	ctx = fsapi.WithContext(ctx, fsRegistry)
	L.SetContext(ctx)

	// Import test script
	err := vm.Import(`
		function test_file_errors()
			local fs = require("fs")
			local fsObj, err = fs.get("test_fs")
			if err then error(err) end

			-- Try to open a non-existent file
			local nonexistent, err_nonexistent = fsObj:open("nonexistent.txt", "r")
			
			-- Try to open a file with a simulated error
			local error_file, err_simulated = fsObj:open("error_file.txt", "r")
			
			-- Try to use a closed file
			local normal_file, _ = fsObj:open("normal_test.txt", "w")
			normal_file:close()
			local _, err_closed = normal_file:read()
			
			return {
				nonexistent_error = err_nonexistent or "",
				simulated_error = err_simulated or "",
				closed_error = err_closed or ""
			}
		end
	`, "test", "test_file_errors")
	require.NoError(t, err)

	// Execute the test function
	result, err := runner.Execute(L.Context(), "test_file_errors")
	require.NoError(t, err)

	// Verify the result
	resultTable := result.(*lua.LTable)
	nonexistentErr := resultTable.RawGetString("nonexistent_error").(lua.LString)
	simulatedErr := resultTable.RawGetString("simulated_error").(lua.LString)
	closedErr := resultTable.RawGetString("closed_error").(lua.LString)

	assert.Contains(t, string(nonexistentErr), "exist")
	assert.Contains(t, string(simulatedErr), "simulated error")
	assert.Contains(t, string(closedErr), "closed")
}
