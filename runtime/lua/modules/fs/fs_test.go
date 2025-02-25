package fs

import (
	"errors"
	fsapi "github.com/ponyruntime/pony/api/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

// TestFSExists tests the fs:exists function
func TestFSExists(t *testing.T) {
	// Create a mock filesystem
	mockFS := newMockFS()
	mockFS.AddFile("exists.txt", "File content")
	mockFS.AddDir("exists_dir")

	// Create a mock resource
	mockRes := &mockResource{
		resValue: mockFS,
	}

	// Create a mock registry
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": mockFS,
		},
	}

	// Setup Lua
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
		function test_fs_exists()
			local fs = require("fs")
			local fsObj, err = fs.get("test_fs")
			if err then error(err) end

			-- Test existing file
			local file_exists, err = fsObj:exists("exists.txt")
			if err then error("Error checking file: " .. err) end

			-- Test existing directory
			local dir_exists, err = fsObj:exists("exists_dir")
			if err then error("Error checking directory: " .. err) end

			-- Test non-existent path
			local nonexistent_exists, err = fsObj:exists("nonexistent")
			if err then error("Error checking nonexistent path: " .. err) end

			return {
				file_exists = file_exists,
				dir_exists = dir_exists,
				nonexistent_exists = nonexistent_exists
			}
		end
	`, "test", "test_fs_exists")
	require.NoError(t, err)

	// Execute the test function
	result, err := runner.Execute(L.Context(), "test_fs_exists")
	require.NoError(t, err)

	// Verify the result
	resultTable := result.(*lua.LTable)
	fileExists := resultTable.RawGetString("file_exists").(lua.LBool)
	dirExists := resultTable.RawGetString("dir_exists").(lua.LBool)
	nonexistentExists := resultTable.RawGetString("nonexistent_exists").(lua.LBool)

	assert.True(t, bool(fileExists))
	assert.True(t, bool(dirExists))
	assert.False(t, bool(nonexistentExists))
}

// TestFSIsDir tests the fs:isdir function
func TestFSIsDir(t *testing.T) {
	// Create a mock filesystem
	mockFS := newMockFS()
	mockFS.AddFile("file.txt", "File content")
	mockFS.AddDir("directory")

	// Create a mock resource
	mockRes := &mockResource{
		resValue: mockFS,
	}

	// Create a mock registry
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": mockFS,
		},
	}

	// Setup Lua
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
		function test_fs_isdir()
			local fs = require("fs")
			local fsObj, err = fs.get("test_fs")
			if err then error(err) end

			-- Test on a file
			local file_is_dir, err = fsObj:isdir("file.txt")
			if err then error("Error checking file: " .. err) end

			-- Test on a directory
			local dir_is_dir, err = fsObj:isdir("directory")
			if err then error("Error checking directory: " .. err) end

			return {
				file_is_dir = file_is_dir,
				dir_is_dir = dir_is_dir
			}
		end
	`, "test", "test_fs_isdir")
	require.NoError(t, err)

	// Execute the test function
	result, err := runner.Execute(L.Context(), "test_fs_isdir")
	require.NoError(t, err)

	// Verify the result
	resultTable := result.(*lua.LTable)
	fileIsDir := resultTable.RawGetString("file_is_dir").(lua.LBool)
	dirIsDir := resultTable.RawGetString("dir_is_dir").(lua.LBool)

	assert.False(t, bool(fileIsDir))
	assert.True(t, bool(dirIsDir))
}

// TestFSPwd tests the fs:pwd function
func TestFSPwd(t *testing.T) {
	// Create a mock filesystem
	mockFS := newMockFS()
	mockFS.AddDir("subdir")

	// Create a mock resource
	mockRes := &mockResource{
		resValue: mockFS,
	}

	// Create a mock registry
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": mockFS,
		},
	}

	// Setup Lua
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
		function test_fs_pwd()
			local fs = require("fs")
			local fsObj, err = fs.get("test_fs")
			if err then error(err) end

			-- Get initial pwd
			local initial_pwd, err = fsObj:pwd()
			if err then error("Error getting initial pwd: " .. err) end

			-- Change directory
			local ok, err = fsObj:chdir("/subdir")
			if err then error("Error changing directory: " .. err) end

			-- Get new pwd
			local new_pwd, err = fsObj:pwd()
			if err then error("Error getting new pwd: " .. err) end

			return {
				initial_pwd = initial_pwd,
				new_pwd = new_pwd
			}
		end
	`, "test", "test_fs_pwd")
	require.NoError(t, err)

	// Execute the test function
	result, err := runner.Execute(L.Context(), "test_fs_pwd")
	require.NoError(t, err)

	// Verify the result
	resultTable := result.(*lua.LTable)
	initialPwd := resultTable.RawGetString("initial_pwd").(lua.LString)
	newPwd := resultTable.RawGetString("new_pwd").(lua.LString)

	assert.Equal(t, "/", string(initialPwd))
	assert.Equal(t, "/subdir", string(newPwd))
}

// TestFSChdir tests the fs:chdir function
func TestFSChdir(t *testing.T) {
	// Create a mock filesystem
	mockFS := newMockFS()
	mockFS.AddDir("parent")
	mockFS.AddDir("parent/child")
	mockFS.AddFile("parent/file.txt", "File in parent")
	mockFS.AddFile("parent/child/file.txt", "File in child")

	// Create a mock resource
	mockRes := &mockResource{
		resValue: mockFS,
	}

	// Create a mock registry
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": mockFS,
		},
	}

	// Setup Lua
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
		function test_fs_chdir()
			local fs = require("fs")
			local fsObj, err = fs.get("test_fs")
			if err then error(err) end

			-- Check initial directory
			local initial_pwd, err = fsObj:pwd()
			if err then error("Error getting initial pwd: " .. err) end

			-- Change to parent
			local ok, err = fsObj:chdir("/parent")
			if err then error("Error changing to parent: " .. err) end

			local parent_pwd, err = fsObj:pwd()
			if err then error("Error getting parent pwd: " .. err) end

			-- Check file in parent is accessible
			local file_exists, err = fsObj:exists("file.txt")
			if err then error("Error checking file existence: " .. err) end

			-- Change to child
			local ok, err = fsObj:chdir("child")
			if err then error("Error changing to child: " .. err) end

			local child_pwd, err = fsObj:pwd()
			if err then error("Error getting child pwd: " .. err) end

			-- Check file in child is accessible
			local child_file_exists, err = fsObj:exists("file.txt")
			if err then error("Error checking child file existence: " .. err) end

			-- Change back to root with absolute path
			local ok, err = fsObj:chdir("/")
			if err then error("Error changing to root: " .. err) end

			local root_pwd, err = fsObj:pwd()
			if err then error("Error getting root pwd: " .. err) end

			return {
				initial_pwd = initial_pwd,
				parent_pwd = parent_pwd,
				parent_file_exists = file_exists,
				child_pwd = child_pwd,
				child_file_exists = child_file_exists,
				root_pwd = root_pwd
			}
		end
	`, "test", "test_fs_chdir")
	require.NoError(t, err)

	// Execute the test function
	result, err := runner.Execute(L.Context(), "test_fs_chdir")
	require.NoError(t, err)

	// Verify the result
	resultTable := result.(*lua.LTable)
	initialPwd := resultTable.RawGetString("initial_pwd").(lua.LString)
	parentPwd := resultTable.RawGetString("parent_pwd").(lua.LString)
	parentFileExists := resultTable.RawGetString("parent_file_exists").(lua.LBool)
	childPwd := resultTable.RawGetString("child_pwd").(lua.LString)
	childFileExists := resultTable.RawGetString("child_file_exists").(lua.LBool)
	rootPwd := resultTable.RawGetString("root_pwd").(lua.LString)

	assert.Equal(t, "/", string(initialPwd))
	assert.Equal(t, "/parent", string(parentPwd))
	assert.True(t, bool(parentFileExists))
	assert.Equal(t, "/parent/child", string(childPwd))
	assert.True(t, bool(childFileExists))
	assert.Equal(t, "/", string(rootPwd))
}

// TestFSMkdir tests the fs:mkdir function
func TestFSMkdir(t *testing.T) {
	// Create a mock filesystem
	mockFS := newMockFS()
	mockFS.AddDir("existing_dir")

	// Create a mock resource
	mockRes := &mockResource{
		resValue: mockFS,
	}

	// Create a mock registry
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": mockFS,
		},
	}

	// Setup Lua
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
		function test_fs_mkdir()
			local fs = require("fs")
			local fsObj, err = fs.get("test_fs")
			if err then error(err) end

			-- Create a new directory
			local success, err = fsObj:mkdir("new_dir")
			if err then error("Error creating directory: " .. err) end

			-- Check that it exists
			local dir_exists, err = fsObj:exists("new_dir")
			if err then error("Error checking directory existence: " .. err) end

			local is_dir, err = fsObj:isdir("new_dir")
			if err then error("Error checking if directory: " .. err) end

			-- Try to create an existing directory
			local existing_success, existing_err = fsObj:mkdir("existing_dir")
			
			-- Create nested directories
			local nested_success, err = fsObj:mkdir("new_dir/nested")
			if err then error("Error creating nested directory: " .. err) end

			local nested_exists, err = fsObj:exists("new_dir/nested")
			if err then error("Error checking nested directory existence: " .. err) end

			return {
				success = success,
				dir_exists = dir_exists,
				is_dir = is_dir,
				existing_success = existing_success or false,
				existing_error = existing_err or "",
				nested_success = nested_success,
				nested_exists = nested_exists
			}
		end
	`, "test", "test_fs_mkdir")
	require.NoError(t, err)

	// Execute the test function
	result, err := runner.Execute(L.Context(), "test_fs_mkdir")
	require.NoError(t, err)

	// Verify the result
	resultTable := result.(*lua.LTable)
	success := resultTable.RawGetString("success").(lua.LBool)
	dirExists := resultTable.RawGetString("dir_exists").(lua.LBool)
	isDir := resultTable.RawGetString("is_dir").(lua.LBool)
	existingSuccess := resultTable.RawGetString("existing_success").(lua.LBool)
	existingError := resultTable.RawGetString("existing_error").(lua.LString)
	nestedSuccess := resultTable.RawGetString("nested_success").(lua.LBool)
	nestedExists := resultTable.RawGetString("nested_exists").(lua.LBool)

	assert.True(t, bool(success))
	assert.True(t, bool(dirExists))
	assert.True(t, bool(isDir))
	assert.False(t, bool(existingSuccess))
	assert.Contains(t, string(existingError), "exists")
	assert.True(t, bool(nestedSuccess))
	assert.True(t, bool(nestedExists))
}

// TestFSRemove tests the fs:remove function
func TestFSRemove(t *testing.T) {
	// Create a mock filesystem
	mockFS := newMockFS()
	mockFS.AddFile("file_to_remove.txt", "File content")
	mockFS.AddDir("dir_to_remove")
	mockFS.AddDir("non_empty_dir")
	mockFS.AddFile("non_empty_dir/file.txt", "File content")

	// Create a mock resource
	mockRes := &mockResource{
		resValue: mockFS,
	}

	// Create a mock registry
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": mockFS,
		},
	}

	// Setup Lua
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
		function test_fs_remove()
			local fs = require("fs")
			local fsObj, err = fs.get("test_fs")
			if err then error(err) end

			-- Remove a file
			local file_success, err = fsObj:remove("file_to_remove.txt")
			if err then error("Error removing file: " .. err) end

			local file_exists, err = fsObj:exists("file_to_remove.txt")
			if err then error("Error checking file existence: " .. err) end

			-- Remove an empty directory
			local dir_success, err = fsObj:remove("dir_to_remove")
			if err then error("Error removing directory: " .. err) end

			local dir_exists, err = fsObj:exists("dir_to_remove")
			if err then error("Error checking directory existence: " .. err) end

			-- Try to remove a non-existent path
			local nonexistent_success, nonexistent_err = fsObj:remove("nonexistent")
			
			-- Try to remove a non-empty directory
			local non_empty_success, non_empty_err = fsObj:remove("non_empty_dir")

			return {
				file_success = file_success,
				file_exists = file_exists,
				dir_success = dir_success,
				dir_exists = dir_exists,
				nonexistent_success = nonexistent_success or false,
				nonexistent_error = nonexistent_err or "",
				non_empty_success = non_empty_success or false,
				non_empty_error = non_empty_err or ""
			}
		end
	`, "test", "test_fs_remove")
	require.NoError(t, err)

	// Execute the test function
	result, err := runner.Execute(L.Context(), "test_fs_remove")
	require.NoError(t, err)

	// Verify the result
	resultTable := result.(*lua.LTable)
	fileSuccess := resultTable.RawGetString("file_success").(lua.LBool)
	fileExists := resultTable.RawGetString("file_exists").(lua.LBool)
	dirSuccess := resultTable.RawGetString("dir_success").(lua.LBool)
	dirExists := resultTable.RawGetString("dir_exists").(lua.LBool)
	nonexistentSuccess := resultTable.RawGetString("nonexistent_success").(lua.LBool)
	nonexistentError := resultTable.RawGetString("nonexistent_error").(lua.LString)
	nonEmptySuccess := resultTable.RawGetString("non_empty_success").(lua.LBool)
	nonEmptyError := resultTable.RawGetString("non_empty_error").(lua.LString)

	assert.True(t, bool(fileSuccess))
	assert.False(t, bool(fileExists))
	assert.True(t, bool(dirSuccess))
	assert.False(t, bool(dirExists))
	assert.False(t, bool(nonexistentSuccess))
	assert.Contains(t, string(nonexistentError), "exist")
	assert.False(t, bool(nonEmptySuccess))
	assert.Contains(t, string(nonEmptyError), "empty")
}

// TestFSReadDir tests the fs:readdir function
func TestFSReadDir(t *testing.T) {
	// Create a mock filesystem
	mockFS := newMockFS()
	mockFS.AddFile("root_file1.txt", "File content")
	mockFS.AddFile("root_file2.txt", "File content")
	mockFS.AddDir("subdir")
	mockFS.AddFile("subdir/subfile1.txt", "Subfile content")
	mockFS.AddFile("subdir/subfile2.txt", "Subfile content")
	mockFS.AddDir("subdir/subsubdir")

	// Create a mock resource
	mockRes := &mockResource{
		resValue: mockFS,
	}

	// Create a mock registry
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": mockFS,
		},
	}

	// Setup Lua
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
		function test_fs_readdir()
			local fs = require("fs")
			local fsObj, err = fs.get("test_fs")
			if err then error(err) end

			-- Read root directory
			local root_entries = {}
			for entry in fsObj:readdir(".") do
				table.insert(root_entries, {
					name = entry.name,
					type = entry.type
				})
			end

			-- Read subdirectory
			local subdir_entries = {}
			for entry in fsObj:readdir("subdir") do
				table.insert(subdir_entries, {
					name = entry.name,
					type = entry.type
				})
			end

			-- Try to read a non-existent directory
			local nonexistent_success, nonexistent_err = pcall(function()
				for _ in fsObj:readdir("nonexistent") do end
			end)

			-- Try to read a file as a directory
			local file_as_dir_success, file_as_dir_err = pcall(function()
				for _ in fsObj:readdir("root_file1.txt") do end
			end)

			return {
				root_entries = root_entries,
				subdir_entries = subdir_entries,
				nonexistent_success = nonexistent_success,
				nonexistent_error = nonexistent_err or "",
				file_as_dir_success = file_as_dir_success,
				file_as_dir_error = file_as_dir_err or ""
			}
		end
	`, "test", "test_fs_readdir")
	require.NoError(t, err)

	// Execute the test function
	result, err := runner.Execute(L.Context(), "test_fs_readdir")
	require.NoError(t, err)

	// Verify the result
	resultTable := result.(*lua.LTable)
	rootEntries := resultTable.RawGetString("root_entries").(*lua.LTable)
	subdirEntries := resultTable.RawGetString("subdir_entries").(*lua.LTable)
	nonexistentSuccess := resultTable.RawGetString("nonexistent_success").(lua.LBool)
	nonexistentError := resultTable.RawGetString("nonexistent_error").(lua.LString)
	fileAsDirSuccess := resultTable.RawGetString("file_as_dir_success").(lua.LBool)
	fileAsDirError := resultTable.RawGetString("file_as_dir_error").(lua.LString)

	// Check root entries
	var rootEntryNames []string
	var rootEntryTypes []string
	rootEntries.ForEach(func(_ lua.LValue, v lua.LValue) {
		entry := v.(*lua.LTable)
		rootEntryNames = append(rootEntryNames, string(entry.RawGetString("name").(lua.LString)))
		rootEntryTypes = append(rootEntryTypes, string(entry.RawGetString("type").(lua.LString)))
	})

	// Check subdir entries
	var subdirEntryNames []string
	var subdirEntryTypes []string
	subdirEntries.ForEach(func(_ lua.LValue, v lua.LValue) {
		entry := v.(*lua.LTable)
		subdirEntryNames = append(subdirEntryNames, string(entry.RawGetString("name").(lua.LString)))
		subdirEntryTypes = append(subdirEntryTypes, string(entry.RawGetString("type").(lua.LString)))
	})

	assert.Len(t, rootEntryNames, 3) // 2 files and 1 dir
	assert.Contains(t, rootEntryNames, "root_file1.txt")
	assert.Contains(t, rootEntryNames, "root_file2.txt")
	assert.Contains(t, rootEntryNames, "subdir")

	assert.Len(t, subdirEntryNames, 3) // 2 files and 1 dir
	assert.Contains(t, subdirEntryNames, "subfile1.txt")
	assert.Contains(t, subdirEntryNames, "subfile2.txt")
	assert.Contains(t, subdirEntryNames, "subsubdir")

	assert.False(t, bool(nonexistentSuccess))
	assert.Contains(t, string(nonexistentError), "exist")
	assert.False(t, bool(fileAsDirSuccess))
	assert.Contains(t, string(fileAsDirError), "not a directory")
}

// TestFSReadFile tests the fs:readfile function
func TestFSReadFile(t *testing.T) {
	// Create a mock filesystem
	mockFS := newMockFS()
	mockFS.AddFile("test.txt", "Hello, World!")
	mockFS.AddDir("dir_to_read")

	// Create a mock resource
	mockRes := &mockResource{
		resValue: mockFS,
	}

	// Create a mock registry
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": mockFS,
		},
	}

	// Setup Lua
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
		function test_fs_readfile()
			local fs = require("fs")
			local fsObj, err = fs.get("test_fs")
			if err then error(err) end

			-- Read a file
			local content, err = fsObj:readfile("test.txt")
			if err then error("Error reading file: " .. err) end

			-- Try to read a non-existent file
			local nonexistent_content, nonexistent_err = fsObj:readfile("nonexistent.txt")

			-- Try to read a directory
			local dir_content, dir_err = fsObj:readfile("dir_to_read")

			return {
				content = content,
				nonexistent_content = nonexistent_content,
				nonexistent_error = nonexistent_err or "",
				dir_content = dir_content,
				dir_error = dir_err or ""
			}
		end
	`, "test", "test_fs_readfile")
	require.NoError(t, err)

	// Execute the test function
	result, err := runner.Execute(L.Context(), "test_fs_readfile")
	require.NoError(t, err)

	// Verify the result
	resultTable := result.(*lua.LTable)
	content := resultTable.RawGetString("content").(lua.LString)
	nonexistentContent := resultTable.RawGetString("nonexistent_content")
	nonexistentError := resultTable.RawGetString("nonexistent_error").(lua.LString)
	dirContent := resultTable.RawGetString("dir_content")
	dirError := resultTable.RawGetString("dir_error").(lua.LString)

	assert.Equal(t, "Hello, World!", string(content))
	assert.Equal(t, lua.LNil, nonexistentContent)
	assert.Contains(t, string(nonexistentError), "exist")
	assert.Equal(t, lua.LNil, dirContent)
	assert.Contains(t, string(dirError), "invalid")
}

// TestFSWriteFile tests the fs:writefile function
func TestFSWriteFile(t *testing.T) {
	// Create a mock filesystem
	mockFS := newMockFS()
	mockFS.AddFile("existing.txt", "Original content")
	mockFS.AddDir("test_dir")

	// Create a mock resource
	mockRes := &mockResource{
		resValue: mockFS,
	}

	// Create a mock registry
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": mockFS,
		},
	}

	// Setup Lua
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
		function test_fs_writefile()
			local fs = require("fs")
			local fsObj, err = fs.get("test_fs")
			if err then error(err) end

			-- Write to a new file
			local success, err = fsObj:writefile("new.txt", "New content")
			if err then error("Error writing new file: " .. err) end

			-- Read it back to verify
			local new_content, err = fsObj:readfile("new.txt")
			if err then error("Error reading new file: " .. err) end

			-- Overwrite an existing file
			local overwrite_success, err = fsObj:writefile("existing.txt", "Updated content")
			if err then error("Error overwriting file: " .. err) end

			-- Read it back to verify
			local updated_content, err = fsObj:readfile("existing.txt")
			if err then error("Error reading updated file: " .. err) end

			-- Try to write to a directory
			local dir_success, dir_err = fsObj:writefile("test_dir", "Directory content")

			-- Test append mode
			local append_success, err = fsObj:writefile("new.txt", " Appended content", "a")
			if err then error("Error appending to file: " .. err) end

			-- Read it back to verify
			local appended_content, err = fsObj:readfile("new.txt")
			if err then error("Error reading appended file: " .. err) end

			-- Test exclusive creation mode
			local exclusive_success, exclusive_err = fsObj:writefile("existing.txt", "Exclusive content", "wx")

			return {
				new_success = success,
				new_content = new_content,
				overwrite_success = overwrite_success,
				updated_content = updated_content,
				dir_success = dir_success or false,
				dir_error = dir_err or "",
				append_success = append_success,
				appended_content = appended_content,
				exclusive_success = exclusive_success or false,
				exclusive_error = exclusive_err or ""
			}
		end
	`, "test", "test_fs_writefile")
	require.NoError(t, err)

	// Execute the test function
	result, err := runner.Execute(L.Context(), "test_fs_writefile")
	require.NoError(t, err)

	// Verify the result
	resultTable := result.(*lua.LTable)
	newSuccess := resultTable.RawGetString("new_success").(lua.LBool)
	newContent := resultTable.RawGetString("new_content").(lua.LString)
	overwriteSuccess := resultTable.RawGetString("overwrite_success").(lua.LBool)
	updatedContent := resultTable.RawGetString("updated_content").(lua.LString)
	dirSuccess := resultTable.RawGetString("dir_success").(lua.LBool)
	dirError := resultTable.RawGetString("dir_error").(lua.LString)
	appendSuccess := resultTable.RawGetString("append_success").(lua.LBool)
	appendedContent := resultTable.RawGetString("appended_content").(lua.LString)
	exclusiveSuccess := resultTable.RawGetString("exclusive_success").(lua.LBool)
	exclusiveError := resultTable.RawGetString("exclusive_error").(lua.LString)

	assert.True(t, bool(newSuccess))
	assert.Equal(t, "New content", string(newContent))
	assert.True(t, bool(overwriteSuccess))
	assert.Equal(t, "Updated content", string(updatedContent))
	assert.False(t, bool(dirSuccess))
	assert.Contains(t, string(dirError), "invalid")
	assert.True(t, bool(appendSuccess))
	assert.Equal(t, "New content Appended content", string(appendedContent))
	assert.False(t, bool(exclusiveSuccess))
	assert.Contains(t, string(exclusiveError), "exist")
}

// TestFSErrorHandling tests error handling in various FS operations
func TestFSErrorHandling(t *testing.T) {
	// Create a mock filesystem
	mockFS := newMockFS()
	// Add specific error cases
	mockFS.AddError("error_path", errors.New("simulated error"))

	// Create a mock resource
	mockRes := &mockResource{
		resValue: mockFS,
	}

	// Create a mock registry
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": mockFS,
		},
	}

	// Setup Lua
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
		function test_fs_error_handling()
			local fs = require("fs")
			local fsObj, err = fs.get("test_fs")
			if err then error(err) end

			-- Test error on exists
			local exists, exists_err = fsObj:exists("error_path")
			
			-- Test error on isdir
			local isdir, isdir_err = fsObj:isdir("error_path")
			
			-- Test error on mkdir
			local mkdir, mkdir_err = fsObj:mkdir("error_path")
			
			-- Test error on remove
			local remove, remove_err = fsObj:remove("error_path")
			
			-- Test error on readdir
			local readdir_success, readdir_err = pcall(function()
				for _ in fsObj:readdir("error_path") do end
			end)
			
			-- Test error on open
			local open, open_err = fsObj:open("error_path", "r")
			
			-- Test error on readfile
			local readfile, readfile_err = fsObj:readfile("error_path")
			
			-- Test error on writefile
			local writefile, writefile_err = fsObj:writefile("error_path", "content")

			return {
				exists_error = exists_err or "",
				isdir_error = isdir_err or "",
				mkdir_error = mkdir_err or "",
				remove_error = remove_err or "",
				readdir_success = readdir_success,
				readdir_error = readdir_err or "",
				open_error = open_err or "",
				readfile_error = readfile_err or "",
				writefile_error = writefile_err or ""
			}
		end
	`, "test", "test_fs_error_handling")
	require.NoError(t, err)

	// Execute the test function
	result, err := runner.Execute(L.Context(), "test_fs_error_handling")
	require.NoError(t, err)

	// Verify the result
	resultTable := result.(*lua.LTable)
	existsError := resultTable.RawGetString("exists_error").(lua.LString)
	isdirError := resultTable.RawGetString("isdir_error").(lua.LString)
	mkdirError := resultTable.RawGetString("mkdir_error").(lua.LString)
	removeError := resultTable.RawGetString("remove_error").(lua.LString)
	readdirSuccess := resultTable.RawGetString("readdir_success").(lua.LBool)
	readdirError := resultTable.RawGetString("readdir_error").(lua.LString)
	openError := resultTable.RawGetString("open_error").(lua.LString)
	readfileError := resultTable.RawGetString("readfile_error").(lua.LString)
	writefileError := resultTable.RawGetString("writefile_error").(lua.LString)

	assert.Contains(t, string(existsError), "simulated error")
	assert.Contains(t, string(isdirError), "simulated error")
	assert.Contains(t, string(mkdirError), "simulated error")
	assert.Contains(t, string(removeError), "simulated error")
	assert.False(t, bool(readdirSuccess))
	assert.Contains(t, string(readdirError), "simulated error")
	assert.Contains(t, string(openError), "simulated error")
	assert.Contains(t, string(readfileError), "simulated error")
	assert.Contains(t, string(writefileError), "simulated error")
}
