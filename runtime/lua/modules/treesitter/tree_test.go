package treesitter

import (
	"github.com/ponyruntime/go-lua"
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

func TestTreeAdditionalMethods(t *testing.T) {
	logger := zap.NewNop()

	t.Run("tree error handling", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local treesitter = require("treesitter")
			
			-- Test invalid language
			local ok, err = pcall(function()
				treesitter.parse("invalid_lang", "code")
			end)
			assert(not ok, "should error for invalid language")
			assert(string.match(err, "unsupported language"), "should mention unsupported language")
			
			-- Test empty code
			local empty_tree = treesitter.parse("go", "")
			assert(empty_tree == nil, "should return nil for empty code")
			
			-- Test invalid syntax
			local code = "func main() { invalid syntax"
			local tree = treesitter.parse("go", code)
			assert(tree ~= nil, "should return tree even with invalid syntax")
			local root = tree:root_node()
			assert(root:has_error(), "should detect syntax error")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("tree gc", func(t *testing.T) {
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
			
			-- Force garbage collection
			tree = nil
			collectgarbage()
		`, "test")
		assert.NoError(t, err)
	})
}

func TestTreeTraversal(t *testing.T) {
	logger := zap.NewNop()

	t.Run("structured code traversal", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
			engine.WithGlobalFunction("print", func(l *lua.LState) int {
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

				type Person struct {
					Name string
					Age  int
				}

				func (p Person) IsAdult() bool {
					return p.Age >= 18
				}
			]]

			-- Parse and basic validation
			local tree = treesitter.parse("go", code)
			assert(tree ~= nil, "tree should not be nil")
			local root = tree:root_node()
			assert(root ~= nil, "root should not be nil")
			assert(root:kind() == "source_file", "root should be source_file")

			-- Test cursor navigation to find key nodes
			local cursor = tree:walk()
			
			-- Find package declaration
			assert(cursor:goto_first_child(), "should move to first child")
			assert(cursor:current_node():kind() == "package_clause", "first node should be package_clause")
			
			-- Find type declaration
			assert(cursor:goto_next_sibling(), "should move to type declaration")
			assert(cursor:current_node():kind() == "type_declaration", "should find type declaration")
			
			-- Step into type declaration to find struct
			assert(cursor:goto_first_child(), "should enter type declaration")
			-- Navigate through type_spec
			local found_type_spec = false
			repeat
				local node = cursor:current_node()
				if node:kind() == "type_spec" then
					found_type_spec = true
					break
				end
			until not cursor:goto_next_sibling()
			assert(found_type_spec, "should find type_spec")
			
			-- Now look for struct_type in type_spec
			assert(cursor:goto_first_child(), "should enter type_spec")
			local found_struct = false
			repeat
				local node = cursor:current_node()
				if node:kind() == "struct_type" then
					found_struct = true
					break
				end
			until not cursor:goto_next_sibling()
			assert(found_struct, "should find struct_type")
			
			-- Go back to root for next test
			cursor:reset(root)
			
			-- Find method declaration
			assert(cursor:goto_first_child(), "should move to first child")
			local found_method = false
			repeat
				local node = cursor:current_node()
				if node:kind() == "method_declaration" then
					found_method = true
					break
				end
			until not cursor:goto_next_sibling()
			assert(found_method, "should find method declaration")

			-- Test cursor copy and independence
			-- First, reset to root and navigate to type_declaration which has both parent and children
			cursor:reset(root)
			assert(cursor:goto_first_child(), "move to first child")
			assert(cursor:goto_next_sibling(), "move to type_declaration")
			
			-- Create copy at type_declaration
			local cursor2 = cursor:copy()
			local original_kind = cursor:current_node():kind()
			assert(cursor2:current_node():kind() == original_kind, "copied cursor should match")
			assert(original_kind == "type_declaration", "should be at type_declaration")
			
			-- Move cursors in different ways
			assert(cursor:goto_first_child(), "first cursor should move down")  -- Should move to 'type'
			assert(cursor2:goto_parent(), "second cursor should move up")      -- Should move to 'source_file'
			
			-- Verify they're at different positions
			local kind1 = cursor:current_node():kind()
			local kind2 = cursor2:current_node():kind()
			assert(kind1 ~= kind2, "cursors should be at different nodes")
			assert(kind1 == "type", "first cursor should be at type")
			assert(kind2 == "source_file", "second cursor should be at source_file")

			-- Test node properties
			local method_node = cursor2:current_node()
			assert(method_node:is_named(), "method node should be named")
			assert(not method_node:has_error(), "method node should not have errors")
			assert(method_node:child_count() > 0, "method node should have children")
			
			-- Test node start/end positions
			local start_point = method_node:start_point()
			local end_point = method_node:end_point()
			assert(type(start_point.row) == "number", "start point should have row")
			assert(type(start_point.column) == "number", "start point should have column")
			assert(end_point.row >= start_point.row, "end row should be >= start row")
		`, "test")
		assert.NoError(t, err)
	})
}
