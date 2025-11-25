//go:build !windows

package treesitter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"go.uber.org/zap"
)

func newTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
}

func TestCursorMethods(t *testing.T) {
	logger := zap.NewNop()

	t.Run("cursor creation and metadata", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local treesitter = require("treesitter")
			local code = [[
				package main
				
				func hello() {
					println("world")
				}
			]]
			local tree = treesitter.parse("go", code)
			assert(tree ~= nil, "tree should not be nil")
			
			-- Spawn cursor and test initial state
			local cursor = tree:walk()
			assert(cursor ~= nil, "cursor should not be nil")
			assert(type(cursor) == "userdata", "cursor should be userdata")
			
			-- Test metadata methods
			local node = cursor:current_node()
			assert(node ~= nil, "current node should not be nil")
			
			local depth = cursor:current_depth()
			assert(type(depth) == "number", "depth should be a number")
			assert(depth == 0, "initial depth should be 0")
			
			local desc_index = cursor:current_descendant_index()
			assert(type(desc_index) == "number", "descendant index should be a number")
			assert(desc_index == 0, "initial descendant index should be 0")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("cursor navigation", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local treesitter = require("treesitter")
			local code = [[
				package main
				
				func hello() {
					println("world")
				}
			]]
			local tree = treesitter.parse("go", code)
			local cursor = tree:walk()
			
			-- Test basic navigation
			local has_child = cursor:goto_first_child()
			assert(has_child == true, "root should have children")
			assert(cursor:current_depth() == 1, "depth should be 1 after going to child")
			
			local has_last = cursor:goto_last_child()
			assert(cursor:current_depth() > 1, "depth should increase after goto_last_child")
			
			local has_parent = cursor:goto_parent()
			assert(has_parent == true, "should return to parent")
			
			local has_next = cursor:goto_next_sibling()
			if has_next then
				local has_prev = cursor:goto_previous_sibling()
				assert(has_prev == true, "should return to previous sibling")
			end
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("cursor reset and copy", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local treesitter = require("treesitter")
			local code = "package main"
			local tree = treesitter.parse("go", code)
			local cursor = tree:walk()
			
			-- Move cursor down the tree
			cursor:goto_first_child()
			local depth = cursor:current_depth()
			assert(depth > 0, "cursor should have moved down")
			
			-- GetField the root node from the tree for resetting
			local root = tree:root_node()
			assert(root ~= nil, "should have valid root node")
			
			-- Reset cursor to root
			cursor:reset(root)
			assert(cursor:current_depth() == 0, "depth should be 0 after reset")
			
			-- Test cursor copy
			local copied = cursor:copy()
			assert(copied ~= nil, "copied cursor should not be nil")
			assert(type(copied) == "userdata", "copied cursor should be userdata")
			
			-- Move original cursor and verify copy is independent
			cursor:goto_first_child()
			assert(cursor:current_depth() ~= copied:current_depth(), "cursors should be independent")
			
			-- Test reset_to
			cursor:reset_to(copied)
			assert(cursor:current_depth() == copied:current_depth(), "depths should match after reset_to")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("cursor positioning by byte and point", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local treesitter = require("treesitter")
			local code = [[
				package main
				
				func hello() {
					println("world")
				}
			]]
			local tree = treesitter.parse("go", code)
			local cursor = tree:walk()
			
			-- Test goto_first_child_for_byte
			local child_index = cursor:goto_first_child_for_byte(15) -- Position after "package"
			assert(child_index == nil or type(child_index) == "number", "child_index should be nil or number")
			
			-- Test goto_first_child_for_point
			local point = {row = 0, column = 8}
			local point_index = cursor:goto_first_child_for_point(point)
			assert(point_index == nil or type(point_index) == "number", "point_index should be nil or number")
			
			-- Test goto_descendant
			cursor:goto_descendant(2) -- Go to arbitrary descendant
			assert(cursor:current_descendant_index() >= 2, "should move to descendant")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestCursorAdditionalMethods(t *testing.T) {
	logger := zap.NewNop()

	t.Run("cursor field operations", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local treesitter = require("treesitter")
			local code = [[
				type Person struct {
					Alias string
					Age  int
				}
			]]
			local tree = treesitter.parse("go", code)
			local cursor = tree:walk()
			
			-- Test field operations
			local field_id = cursor:current_field_id()
			assert(type(field_id) == "number", "field_id should be number")
			
			local field_name = cursor:current_field_name()
			assert(field_name == nil or type(field_name) == "string", "field_name should be nil or string")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("cursor navigation edge cases", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local treesitter = require("treesitter")
			local code = "package main"
			local tree = treesitter.parse("go", code)
			local cursor = tree:walk()
			
			-- Test navigation at boundaries
			cursor:goto_first_child()
			local result = cursor:goto_previous_sibling()
			assert(result == false, "should not go to previous sibling at start")
			
			cursor:goto_parent()
			cursor:goto_last_child()
			result = cursor:goto_next_sibling()
			assert(result == false, "should not go to next sibling at end")
			
			-- Test invalid descendant index
			cursor:goto_descendant(9999)  -- Should not crash
			
			-- Test invalid byte/point positions
			local idx = cursor:goto_first_child_for_byte(9999)
			assert(idx == nil, "should handle invalid byte position")
			
			local point_idx = cursor:goto_first_child_for_point({row = 999, column = 999})
			assert(point_idx == nil, "should handle invalid point position")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("cursor gc", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local treesitter = require("treesitter")
			local code = "package main"
			local tree = treesitter.parse("go", code)
			local cursor = tree:walk()
			
			-- Force garbage collection
			cursor = nil
			collectgarbage()
		`, "test")
		assert.NoError(t, err)
	})
}

func TestCursorImplementation(t *testing.T) {
	logger := zap.NewNop()

	t.Run("basic cursor movement", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
            local treesitter = require("treesitter")
            local code = "package main\n\nfunc test() {}\n"
            local tree = treesitter.parse("go", code)
            local root = tree:root_node()
            local cursor = tree:walk()

            -- Try first child movement
            local success = cursor:goto_first_child()
            assert(success, "should be able to move to first child")
            assert(cursor:current_node():kind() == "package_clause", "should be at package_clause")

            -- Try next sibling movement
            success = cursor:goto_next_sibling()
            assert(success, "should be able to move to next sibling")
            assert(cursor:current_node():kind() == "function_declaration", "should be at function_declaration")

            -- Test movement back up
            success = cursor:goto_parent()
            assert(success, "should be able to move back to parent")
            assert(cursor:current_node():kind() == "source_file", "should be back at source_file")
        `, "test")
		assert.NoError(t, err)
	})

	t.Run("cursor navigation with reset", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
            local treesitter = require("treesitter")
            local code = "package main\n\nfunc test() {}\n"
            local tree = treesitter.parse("go", code)
            local root = tree:root_node()
            local cursor = tree:walk()

            -- Move cursor down
            assert(cursor:goto_first_child(), "should move to first child")
            local before_reset = cursor:current_node():kind()

            -- Reset cursor to root
            cursor:reset(root)
            local after_reset = cursor:current_node():kind()

            -- Should be back at root
            assert(after_reset == "source_file", "should be back at source_file after reset")
            assert(before_reset ~= after_reset, "position should change after reset")
        `, "test")
		assert.NoError(t, err)
	})
}
