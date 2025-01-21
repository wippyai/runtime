package lfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	apic "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func assertLua(l *lua.LState) int {
	if l.ToBool(1) {
		return 0
	}
	l.RaiseError("%s", l.OptString(2, "assertion failed!"))
	return 0
}

func TestLFS(t *testing.T) {
	// Test to verify that Init cannot handle arguments yet
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	ctx = context.WithValue(ctx, apic.LoggerCtx, logger)

	mod := NewLFSModule()
	vm, err := engine.NewVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithGlobalFunction("assert", assertLua),
	)
	require.NoError(t, err)
	defer vm.Close()

	initialDir, err := os.Getwd()
	require.NoError(t, err)

	err = vm.DoString(ctx, `
			local lfs = require("lfs")
			local current = lfs.currentdir()
			assert(current ~= nil, "currentdir should return a path")
			
			-- Try changing to parent directory
			local success = lfs.chdir("..")
			assert(success == true, "chdir should succeed")
			
			local newCurrent = lfs.currentdir()
			assert(current ~= newCurrent, "directory should have changed")
			
			-- Change back
			success = lfs.chdir(current)
			assert(success == true, "chdir back should succeed")
		`, "test")
	assert.NoError(t, err)

	// Verify we're back where we started
	endDir, err := os.Getwd()
	require.NoError(t, err)
	assert.Equal(t, initialDir, endDir)
}

func TestLFSModuleDoString(t *testing.T) {
	// Test to verify that Init cannot handle arguments yet
	logger, _ := zap.NewDevelopment()

	ctx := context.Background()
	ctx = context.WithValue(ctx, apic.LoggerCtx, logger)

	t.Run("module creation and loading", func(t *testing.T) {
		mod := NewLFSModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(ctx, `
			local lfs = require("lfs")
			assert(type(lfs) == "table")
			assert(type(lfs.attributes) == "function")
			assert(type(lfs.chdir) == "function")
			assert(type(lfs.currentdir) == "function")
			assert(type(lfs.dir) == "function")
			assert(type(lfs.link) == "function")
			assert(type(lfs.mkdir) == "function")
			assert(type(lfs.rmdir) == "function")
			assert(type(lfs.touch) == "function")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("currentdir and chdir", func(t *testing.T) {
		mod := NewLFSModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		initialDir, err := os.Getwd()
		require.NoError(t, err)

		err = vm.DoString(ctx, `
			local lfs = require("lfs")
			local current = lfs.currentdir()
			assert(current ~= nil, "currentdir should return a path")
			
			-- Try changing to parent directory
			local success = lfs.chdir("..")
			assert(success == true, "chdir should succeed")
			
			local newCurrent = lfs.currentdir()
			assert(current ~= newCurrent, "directory should have changed")
			
			-- Change back
			success = lfs.chdir(current)
			assert(success == true, "chdir back should succeed")
		`, "test")
		assert.NoError(t, err)

		// Verify we're back where we started
		endDir, err := os.Getwd()
		require.NoError(t, err)
		assert.Equal(t, initialDir, endDir)
	})

	t.Run("mkdir and rmdir", func(t *testing.T) {
		mod := NewLFSModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		tempDir := t.TempDir()
		testDir := filepath.Join(tempDir, "testdir")

		err = vm.Import(`
			local lfs = require("lfs")
			function mkdir_rmdir_test(dirpath)
				local success = lfs.mkdir(dirpath)
				assert(success == true, "mkdir should succeed")
				
				local attr = lfs.attributes(dirpath)
				assert(attr.mode == "directory", "should be a directory")
				
				success = lfs.rmdir(dirpath)
				assert(success == true, "rmdir should succeed")
				
				attr = lfs.attributes(dirpath)
				assert(attr == nil, "directory should not exist after removal")
			end
			return mkdir_rmdir_test
		`, "mkdir_rmdir_test_file", "mkdir_rmdir_test")
		require.NoError(t, err)

		_, err = vm.Execute(ctx, "mkdir_rmdir_test", lua.LString(testDir))
		assert.NoError(t, err)
	})

	t.Run("attributes", func(t *testing.T) {
		mod := NewLFSModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		tempFile := filepath.Join(t.TempDir(), "test.txt")
		err = os.WriteFile(tempFile, []byte("test content"), 0600)
		require.NoError(t, err)

		err = vm.Import(`
			local lfs = require("lfs")
			function attributes_test(filepath)
				local attr = lfs.attributes(filepath)
				assert(attr ~= nil, "attributes should not be nil")
				assert(attr.mode == "file", "should be a file")
				assert(type(attr.size) == "number", "size should be a number")
				assert(type(attr.modification) == "number", "modification time should be a number")
				assert(type(attr.access) == "number", "access time should be a number")
				assert(type(attr.change) == "number", "change time should be a number")
			end
			return attributes_test
		`, "attributes_test", "attributes_test")
		require.NoError(t, err)

		_, err = vm.Execute(ctx, "attributes_test", lua.LString(tempFile))
		assert.NoError(t, err)
	})

	t.Run("touch", func(t *testing.T) {
		mod := NewLFSModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		tempDir := t.TempDir()
		tempFile := filepath.Join(tempDir, "touch_test.txt")

		err = vm.Import(`
			local lfs = require("lfs")
			function touch_test(filepath)
				local success = lfs.touch(filepath)
				assert(success == true, "touch should succeed")

				local attr = lfs.attributes(filepath)
				assert(attr ~= nil, "file should exist after touch")
				assert(attr.mode == "file", "should be a file")
			end
			return touch_test
		`, "touch_test_file", "touch_test")
		require.NoError(t, err)

		_, err = vm.Execute(ctx, "touch_test", lua.LString(tempFile))
		assert.NoError(t, err, "Lua function should execute without error")

		// Verify the file exists using os.Stat (which uses absolute paths)
		_, err = os.Stat(tempFile)
		assert.NoError(t, err, "file should exist")
	})

	t.Run("link", func(t *testing.T) {
		mod := NewLFSModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		tempDir := t.TempDir()
		sourceFile := filepath.Join(tempDir, "source.txt")
		err = os.WriteFile(sourceFile, []byte("test content"), 0600)
		require.NoError(t, err)

		linkFile := filepath.Join(tempDir, "link.txt")

		err = vm.Import(`
			local lfs = require("lfs")
			function link_test(source, link)
				local success = lfs.link(source, link, true)  -- Create symbolic link
				assert(success == true, "link creation should succeed")

				local attr = lfs.symlinkattributes(link)
				assert(attr ~= nil, "link should exist")
				assert(attr.mode == "link", "should be a symbolic link")
			end
			return link_test
		`, "link_test_file", "link_test")
		require.NoError(t, err)

		_, err = vm.Execute(
			ctx,
			"link_test", lua.LString(sourceFile), lua.LString(linkFile))
		assert.NoError(t, err)
	})

	t.Run("dir iterator", func(t *testing.T) {
		mod := NewLFSModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		tempDir := t.TempDir()
		// Create some files
		files := []string{"file1.txt", "file2.txt", "file3.txt"}
		for _, f := range files {
			err = os.WriteFile(filepath.Join(tempDir, f), []byte("test"), 0600)
			require.NoError(t, err)
		}

		err = vm.Import(`
			local lfs = require("lfs")
			function dir_test(dirpath)
				local iter, dir = lfs.dir(dirpath)
				assert(type(iter) == "function", "dir should return an iterator function")
				
				local count = 0
				for name in iter, dir do
					count = count + 1
				end
				assert(count >= 3, "should find at least 3 entries")
			end
			return dir_test
		`, "dir_test_file", "dir_test")
		require.NoError(t, err)

		_, err = vm.Execute(ctx, "dir_test", lua.LString(tempDir))
		assert.NoError(t, err)
	})
}
