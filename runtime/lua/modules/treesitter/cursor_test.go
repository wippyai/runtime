package treesitter

import (
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func TestCursorMethods(t *testing.T) {
	logger := zap.NewNop()

	t.Run("cursor creation and metadata", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local treesitter = require("treesitter")
			local code = [[
				package main
				
				func hello() {
					println("world")
				}
			]]
			local tree = treesitter.parse("go", code)
			assert(tree ~= nil, "tree should not be nil")
			
			-- Create cursor and test initial state
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
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
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
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local treesitter = require("treesitter")
			local code = "package main"
			local tree = treesitter.parse("go", code)
			local cursor = tree:walk()
			
			-- Move cursor down the tree
			cursor:goto_first_child()
			local depth = cursor:current_depth()
			assert(depth > 0, "cursor should have moved down")
			
			-- Get the root node from the tree for resetting
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
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
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
