package treesitter

import (
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func TestTreeMethods(t *testing.T) {
	logger := zap.NewNop()

	t.Run("tree root node", func(t *testing.T) {
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
			assert(type(tree) == "userdata", "tree should be userdata")
			
			-- Test root_node method
			local root = tree:root_node()
			assert(root ~= nil, "root node should not be nil")
			assert(type(root) == "userdata", "root node should be userdata")

            -- Check if the metatable is set correctly
            local mt = getmetatable(root)
            assert(mt ~= nil, "root node should have metatable")
            assert(mt.__index ~= nil, "metatable should have __index")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("tree copy", func(t *testing.T) {
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
			assert(tree ~= nil, "tree should not be nil")
			
			-- Test copy method
			local copied = tree:copy()
			assert(copied ~= nil, "copied tree should not be nil")
			assert(type(copied) == "userdata", "copied tree should be userdata")
			
			-- Verify both trees can get root nodes
			local root1 = tree:root_node()
			local root2 = copied:root_node()
			assert(root1 ~= nil, "original tree should have root")
			assert(root2 ~= nil, "copied tree should have root")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("tree walk", func(t *testing.T) {
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
			
			-- Test walk method
			local cursor = tree:walk()
			assert(cursor ~= nil, "cursor should not be nil")
			assert(type(cursor) == "userdata", "cursor should be userdata")
		`, "test")
		assert.NoError(t, err)
	})
}
