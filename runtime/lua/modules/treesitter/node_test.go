package treesitter

import (
	"github.com/ponyruntime/go-lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func TestNodeMethods(t *testing.T) {
	logger := zap.NewNop()

	t.Run("node type checking", func(t *testing.T) {
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
			local root = tree:root_node()
			assert(root ~= nil, "root should not be nil")
			
			assert(type(root) == "userdata", "root should be userdata")
			local mt = getmetatable(root)
			assert(mt ~= nil, "node should have metatable")
			assert(mt.__index ~= nil, "node metatable should have __index")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("node child access", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
			engine.WithGlobalFunction("println", func(l *lua.LState) int {
				top := l.GetTop()
				for i := 1; i <= top; i++ {
					t.Logf("%s", l.Get(i).String())
				}
				return 0
			}),
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
			local root = tree:root_node()
			assert(root ~= nil, "root should not be nil")

			-- Test basic child access
			local child_count = root:child_count()
			assert(type(child_count) == "number", "child_count should return number")
			assert(child_count > 0, "root should have children")

			local first_child = root:child(0)
			assert(first_child ~= nil, "should get first child")
			assert(type(first_child) == "userdata", "child should be userdata")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("node named child access", func(t *testing.T) {
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
			local root = tree:root_node()
			assert(root ~= nil, "root should not be nil")
			
			local named_count = root:named_child_count()
			assert(type(named_count) == "number", "named_child_count should return number")
			
			if named_count > 0 then
				local named_child = root:named_child(0)
				assert(named_child ~= nil, "should get named child")
				assert(type(named_child) == "userdata", "named child should be userdata")
			end
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("node parent access", func(t *testing.T) {
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
			local root = tree:root_node()
			assert(root ~= nil, "root should not be nil")
			
			local child = root:child(0)
			assert(child ~= nil, "should get child")
			
			local parent = child:parent()
			assert(parent ~= nil, "child should have parent")
			assert(type(parent) == "userdata", "parent should be userdata")
		`, "test")
		assert.NoError(t, err)
	})
}
