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

func TestNodeAdditionalMethods(t *testing.T) {
	logger := zap.NewNop()

	t.Run("node field operations", func(t *testing.T) {
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
				type Person struct {
					Name string
					Age  int
				}
			]]
			local tree = treesitter.parse("go", code)
			local root = tree:root_node()
			
			-- Test field name operations
			local struct_node = root:named_child(0)
			assert(struct_node ~= nil, "should get struct node")
			
			local field_child = struct_node:child_by_field_name("name")
			assert(field_child == nil or type(field_child) == "userdata", "field child should be nil or userdata")
			
			local field_name = struct_node:field_name_for_child(0)
			assert(field_name == nil or type(field_name) == "string", "field name should be nil or string")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("node inspection methods", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local treesitter = require("treesitter")
			local code = "func main() { invalid syntax }"
			local tree = treesitter.parse("go", code)
			local root = tree:root_node()
			
			-- Test inspection methods
			local kind = root:kind()
			assert(type(kind) == "string", "kind should be string")
			
			local is_named = root:is_named()
			assert(type(is_named) == "boolean", "is_named should be boolean")
			
			local has_error = root:has_error()
			assert(type(has_error) == "boolean", "has_error should be boolean")
			assert(has_error == true, "should detect syntax error")
			
			local is_error = root:is_error()
			assert(type(is_error) == "boolean", "is_error should be boolean")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("node position methods", func(t *testing.T) {
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
			local root = tree:root_node()
			
			-- Test position methods
			local start_byte = root:start_byte()
			assert(type(start_byte) == "number", "start_byte should be number")
			assert(start_byte == 0, "root should start at byte 0")
			
			local end_byte = root:end_byte()
			assert(type(end_byte) == "number", "end_byte should be number")
			assert(end_byte > start_byte, "end_byte should be greater than start_byte")
			
			local start_point = root:start_point()
			assert(type(start_point) == "table", "start_point should be table")
			assert(type(start_point.row) == "number", "start_point.row should be number")
			assert(type(start_point.column) == "number", "start_point.column should be number")
			
			local end_point = root:end_point()
			assert(type(end_point) == "table", "end_point should be table")
			assert(end_point.row >= start_point.row, "end_point row should be >= start_point row")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestNodeTextMethod(t *testing.T) {
	logger := zap.NewNop()

	t.Run("node text methods", func(t *testing.T) {
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
            local code = [[package main

type Person struct {
    Name string
    Age  int
}]]
            local tree = treesitter.parse("go", code)
            local root = tree:root_node()
            
            -- Test text() on root node
            local root_text = root:text(code)
            assert(root_text == code, "root text should match original code")
            
            -- Test text() on package declaration
            local cursor = tree:walk()
            cursor:goto_first_child()
            local pkg_node = cursor:current_node()
            local pkg_text = pkg_node:text(code)
            assert(pkg_text == "package main", "package text should match")
            
            -- Test text() on struct field
            cursor:reset(root)
            cursor:goto_first_child()
            assert(cursor:goto_next_sibling(), "move to type_declaration")
            assert(cursor:goto_first_child(), "enter type_declaration")
            assert(cursor:goto_next_sibling(), "move to type_spec")
            assert(cursor:goto_first_child(), "enter type_spec")
            assert(cursor:goto_next_sibling(), "move to struct_type")
            assert(cursor:goto_first_child(), "enter struct")
            assert(cursor:goto_next_sibling(), "move to field_list")
            assert(cursor:goto_first_child(), "enter first field")
            assert(cursor:goto_next_sibling(), "move to field_declaration")
            
            local field_node = cursor:current_node()
            local field_text = field_node:text(code)
            assert(field_text:match("Name%s+string"), "field text should contain Name string")
            
            -- Test error handling with invalid source
            local ok, err = pcall(function()
                field_node:text("short")
            end)
            assert(not ok, "should fail with invalid source")
            assert(err and err:match("invalid byte range"), "error should mention 'invalid byte range'")
        `, "test")
		require.NoError(t, err)
	})
}

func TestNodeChildText(t *testing.T) {
	logger := zap.NewNop()

	t.Run("child node text extraction", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
            local treesitter = require("treesitter")
            local code = "func test() { return 42 }"
            
            local tree = treesitter.parse("go", code)
            local root = tree:root_node()
            
            -- Get the first child (function_declaration)
            local func_node = root:child(0)
            local func_text = func_node:text(code)
            assert(func_text == "func test() { return 42 }", "function text should match")
            
            -- Get function name node
            local name_node = func_node:child_by_field_name("name")
            local name_text = name_node:text(code)
            assert(name_text == "test", "function name should match")
            
            -- Get function body
            local body_node = func_node:child_by_field_name("body")
            local body_text = body_node:text(code)
            assert(body_text == "{ return 42 }", "function body should match")
        `, "test")
		require.NoError(t, err)
	})
}

func TestOtherNodeMethods(t *testing.T) {
	logger := zap.NewNop()

	t.Run("grammar name and extra nodes", func(t *testing.T) {
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

				// This is a comment
				func hello() {
					println("world")
				}
			]]
			local tree = treesitter.parse("go", code)
			assert(tree ~= nil, "tree should not be nil")
			local root = tree:root_node()
			
			-- Test grammar_name
			local first_child = root:child(0)
			assert(first_child:grammar_name() == "package_clause", "should get correct grammar name")
			
			-- Test is_extra for comment node
			local cursor = tree:walk()
			cursor:goto_first_child()
			while cursor:current_node():kind() ~= "comment" do
				if not cursor:goto_next_sibling() then
					break
				end
			end
			local comment = cursor:current_node()
			assert(comment:is_extra() == true, "comment should be marked as extra")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("error nodes in incomplete syntax", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local treesitter = require("treesitter")
			local code = "func main() { x = "  -- Missing expression and closing brace
			local tree = treesitter.parse("go", code)
			local root = tree:root_node()
			
			-- Test descendant_count
			local count = root:descendant_count()
			assert(count > 0, "should have descendants")
			
			-- Check for error nodes in incomplete syntax
			local found_error = false
			local function check_error(node)
				if node:is_error() then
					found_error = true
					return
				end
				for i = 0, node:child_count() - 1 do
					check_error(node:child(i))
				end
			end
			check_error(root)
			
			assert(found_error, "should find error node in incomplete syntax")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("named descendant for point range", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local treesitter = require("treesitter")
			local code = [[package main
			
func hello() {
	println("world")
}]]
			local tree = treesitter.parse("go", code)
			local root = tree:root_node()
			
			local start_point = {row = 2, column = 0}
			local end_point = {row = 4, column = 1}
			local node = root:named_descendant_for_point_range(start_point, end_point)
			
			assert(node ~= nil, "should find node in point range")
			assert(node:kind() == "function_declaration", "should capture function declaration")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("to sexp representation", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local treesitter = require("treesitter")
			local code = [[package main

func hello() {
	return
}]]
			local tree = treesitter.parse("go", code)
			local root = tree:root_node()
			
			local sexp = root:to_sexp()
			assert(type(sexp) == "string", "should return string representation")
			assert(#sexp > 0, "sexp should not be empty")

			assert(string.find(sexp, "(source_file", 1, true), "should start with source_file")
			assert(string.find(sexp, "package_clause", 1, true), "should contain package_clause")
			assert(string.find(sexp, "function_declaration", 1, true), "should contain function_declaration")
			assert(string.find(sexp, "identifier", 1, true), "should contain identifier")
			
			local cursor = tree:walk()
			cursor:goto_first_child()
			cursor:goto_next_sibling()
			local func_node = cursor:current_node()
			local func_sexp = func_node:to_sexp()
			
			assert(string.find(func_sexp, "(function_declaration", 1, true), "function should start with function_declaration")
			assert(string.find(func_sexp, "name: (identifier)", 1, true), "should have name with identifier")
			assert(string.find(func_sexp, "parameters: (parameter_list)", 1, true), "should have parameter list")
			assert(string.find(func_sexp, "body: (block", 1, true), "should have body block")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestCodeValidation(t *testing.T) {
	logger := zap.NewNop()

	t.Run("detect syntax errors", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local treesitter = require("treesitter")
			
			-- Test case 1: Missing closing brace
			local code1 = [[
package main

func main() {
	fmt.Println("Hello"
]]
			local tree1 = treesitter.parse("go", code1)
			local root1 = tree1:root_node()

			-- Find the error location
			local error_found = false
			local error_line = -1
			local error_col = -1

			local function find_error_node(node)
				print("Checking node: " .. node:kind() .. " (is_error: " .. tostring(node:is_error()) .. ", has_error: " .. tostring(node:has_error()) .. ")")
				if node:is_error() then
					local start_pos = node:start_point()
					error_line = start_pos.row + 1  -- Convert to 1-based line numbers
					error_col = start_pos.column + 1
					error_found = true
					return true
				end
				
				if node:has_error() then
					for i = 0, node:child_count() - 1 do
						local child = node:child(i)
						if find_error_node(child) then
							return true
						end
					end
				end
				return false
			end

			find_error_node(root1)
			assert(error_found, "should find error node")
			assert(error_line == 3, "error should be on line 4")
			
			-- Test case 2: Invalid function declaration
			local code2 = [[
package main

func ) {
	return
}
]]
			local tree2 = treesitter.parse("go", code2)
			local root2 = tree2:root_node()
			
			-- Reset error tracking
			error_found = false
			error_line = -1
			error_col = -1
			find_error_node(root2)
			
			assert(error_found, "should find error in invalid function")
			assert(error_line == 3, "error should be on line 3")
	`, "test")
		assert.NoError(t, err)
	})
}
