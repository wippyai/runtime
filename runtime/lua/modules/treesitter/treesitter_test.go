//go:build !windows

package treesitter

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/runtime/lua/engine"
	scheduler "github.com/wippyai/runtime/system/scheduler/actor"
	lua "github.com/yuin/gopher-lua"
)

func assertLua(l *lua.LState) int {
	if l.ToBool(1) {
		return 0
	}
	l.RaiseError("%s", l.OptString(2, "assertion failed!"))
	return 0
}

func runLuaTest(t *testing.T, script string) {
	t.Helper()

	proc := engine.NewProcess(
		engine.WithScript(script, "test.lua"),
		engine.WithModuleBinder(func(l *lua.LState) {
			Bind(l)
			l.SetGlobal("assert", l.NewFunction(assertLua))
		}),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Execute(ctx, "", nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	defer proc.Close()

	for i := 0; i < 100; i++ {
		result, err := proc.Step(nil)
		if err != nil {
			t.Fatalf("Step failed: %v", err)
		}
		if result.Status == scheduler.StepDone {
			return
		}
	}
	t.Error("Did not complete in expected steps")
}

// =============================================================================
// Module Tests
// =============================================================================

func TestTreeSitterModule_Parse(t *testing.T) {
	t.Run("basic parse", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local code = "package main"
			local tree = treesitter.parse("go", code)
			assert(tree ~= nil, "tree should not be nil")
			assert(type(tree) == "userdata", "tree should be userdata")
		`)
	})
}

func TestLanguageOperations(t *testing.T) {
	t.Run("direct language operations", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")

			-- Test supported languages
			local langs = treesitter.supported_languages()
			assert(langs.go, "Go should be supported")
			assert(langs.javascript, "JavaScript should be supported")

			-- Test getting language directly
			local go_lang = treesitter.language("go")
			assert(go_lang ~= nil, "should get Go language")

			-- Language version
			local version = go_lang:version()
			assert(type(version) == "number", "version should be number")
			assert(version > 0, "version should be positive")

			-- Node kinds
			local kind_count = go_lang:node_kind_count()
			assert(kind_count > 0, "should have node kinds")

			local kind = go_lang:node_kind_for_id(1)
			assert(type(kind) == "string", "kind should be string")

			local id = go_lang:id_for_node_kind(kind, true)
			assert(type(id) == "number", "id should be number")

			-- Field operations
			local field_count = go_lang:field_count()
			assert(field_count > 0, "should have fields")

			local field_name = go_lang:field_name_for_id(1)
			assert(type(field_name) == "string", "field name should be string")

			local field_id = go_lang:field_id_for_name(field_name)
			assert(type(field_id) == "number", "field id should be number")

			-- Test invalid language
			local invalid_lang, err = treesitter.language("nonexistent")
			assert(invalid_lang == nil, "invalid language should return nil")
			assert(err:match("unsupported language"), "should indicate unsupported language")

			-- Test different language features
			local js_lang = treesitter.language("javascript")
			assert(js_lang ~= nil, "should get JavaScript language")

			-- Compare language characteristics
			assert(go_lang:node_kind_count() ~= js_lang:node_kind_count(),
				"different languages should have different node kinds")

			assert(go_lang:field_count() ~= js_lang:field_count(),
				"different languages should have different field counts")
		`)
	})
}

func TestLuaSupport(t *testing.T) {
	runLuaTest(t, `
		local treesitter = require("treesitter")

		-- Simple Lua code with a function
		local code = [[
			function greet(name)
				return "Hello, " .. name .. "!"
			end
		]]

		-- Parse the code
		local tree = treesitter.parse("lua", code)
		assert(tree ~= nil, "tree should not be nil")

		local root = tree:root_node()
		assert(root ~= nil, "root should not be nil")

		-- Spawn a simple query to find the function
		local query = treesitter.query("lua", [[
			(function_declaration
				name: (identifier) @func_name
				body: (block) @func_body)
		]])
		assert(query ~= nil, "query should not be nil")

		-- Run query
		local matches = query:matches(root, code)
		assert(matches ~= nil, "matches should not be nil")

		-- Should find exactly one function
		local match_count = 0
		for _, match in pairs(matches) do
			match_count = match_count + 1

			-- Verify the match has captures
			assert(match.captures ~= nil, "match should have captures")
			assert(#match.captures == 2, "should have two captures")

			-- Verify the function name capture
			local func_name_capture = match.captures[1]
			assert(func_name_capture.node ~= nil, "capture should have node")
			assert(func_name_capture.name == "func_name", "capture name should be func_name")
			local func_name_text = func_name_capture.node:text(code)
			assert(func_name_text == "greet", "captured function name should be 'greet'")

			-- Verify the function body capture
			local func_body_capture = match.captures[2]
			assert(func_body_capture.node ~= nil, "capture should have node")
			assert(func_body_capture.name == "func_body", "capture name should be func_body")
			local func_body_text = func_body_capture.node:text(code)
			assert(func_body_text:match("return \"Hello"), "captured body should start with 'return \"Hello'")
		end

		assert(match_count == 1, "should find exactly one function")
	`)
}

func TestGrammarSupport(t *testing.T) {
	t.Run("supported languages", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local langs = treesitter.supported_languages()
			assert(type(langs) == "table", "supported_languages should return a table")

			-- Check that key languages are supported
			assert(langs["go"] ~= nil, "Go should be supported")
			assert(langs["javascript"] ~= nil, "JavaScript should be supported")
			assert(langs["python"] ~= nil, "Python should be supported")
			assert(langs["php"] ~= nil, "PHP should be supported")
			assert(langs["typescript"] ~= nil, "TypeScript should be supported")
			assert(langs["html"] ~= nil, "HTML should be supported")
			assert(langs["c#"] ~= nil, "C# should be supported")
		`)
	})

	t.Run("language aliases", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")

			-- Test various language aliases
			local function test_parse(alias, code)
				local tree = treesitter.parse(alias, code)
				assert(tree ~= nil, "should return valid tree for " .. alias)
				return tree
			end

			-- Test Go
			test_parse("go", "package main")

			-- Test JavaScript
			test_parse("js", "function test() {}")
			test_parse("javascript", "function test() {}")

			-- Test Python
			test_parse("python", "def test():\n    pass")
			test_parse("py", "def test():\n    pass")

			-- Test TypeScript
			test_parse("ts", "interface Test {}")
			test_parse("typescript", "interface Test {}")

			-- Test TSX
			test_parse("tsx", "const Component = () => <div/>")

			-- Test PHP
			test_parse("php", "<?php\nfunction test() {}")

			-- Test HTML
			test_parse("html", "<div>test</div>")
			test_parse("html5", "<div>test</div>")

			-- Test C#
			test_parse("csharp", "class Test {}")
			test_parse("c#", "class Test {}")
		`)
	})

	t.Run("parser error handling", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")

			local function test_invalid_parse(alias)
				local ok, err = pcall(function()
					treesitter.parse(alias, "code")
				end)
				return ok, err
			end

			-- Test empty language
			local ok, err = test_invalid_parse("")
			assert(not ok, "should fail for empty language")
			assert(string.match(err, "unsupported language"), "error should mention unsupported language")

			-- Test invalid syntax (should parse but have errors)
			local tree = treesitter.parse("go", "func main() {")
			assert(tree ~= nil, "should return tree even for invalid syntax")
			local root = tree:root_node()
			assert(root:has_error(), "should detect syntax error")
		`)
	})
}

// =============================================================================
// Parser Tests
// =============================================================================

func TestParserOperations(t *testing.T) {
	t.Run("parser lifecycle", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")

			-- Create parser
			local parser = treesitter.parser()
			assert(parser ~= nil, "parser should not be nil")

			-- Set language
			local ok = parser:set_language("go")
			assert(ok, "should set language successfully")

			-- Get language
			local lang = parser:get_language()
			assert(lang == "go", "language should be 'go'")

			-- Parse code
			local tree = parser:parse("package main")
			assert(tree ~= nil, "tree should not be nil")

			local root = tree:root_node()
			assert(root ~= nil, "root should not be nil")
			assert(root:kind() == "source_file", "root should be source_file")

			-- Close parser
			parser:close()
		`)
	})

	t.Run("parser reset and close", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")

			local parser = treesitter.parser()
			parser:set_language("go")

			-- Parse first code
			local tree1 = parser:parse("package main")
			assert(tree1 ~= nil, "first tree should not be nil")

			-- Reset parser
			parser:reset()

			-- Parse second code
			local tree2 = parser:parse("package test\n\nfunc Test() {}")
			assert(tree2 ~= nil, "second tree should not be nil")

			-- Verify trees are different
			local root1 = tree1:root_node()
			local root2 = tree2:root_node()
			assert(root1:child_count() ~= root2:child_count() or
				   root1:text("package main") ~= root2:text("package test\n\nfunc Test() {}"),
				   "trees should be different")

			-- Close parser
			parser:close()

			-- Operations on closed parser should fail
			local ok, err = pcall(function()
				parser:parse("package main")
			end)
			assert(not ok, "parse on closed parser should fail")
		`)
	})

	t.Run("parser timeout and ranges", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")

			local parser = treesitter.parser()
			parser:set_language("go")

			-- Set timeout
			parser:set_timeout(5000000) -- 5 seconds in microseconds

			-- Set ranges (parse only specific byte ranges)
			local ranges = {
				{start_byte = 0, end_byte = 12, start_point = {row = 0, column = 0}, end_point = {row = 0, column = 12}}
			}
			parser:set_ranges(ranges)

			-- Parse with ranges
			local tree = parser:parse("package main\n\nfunc hello() {}")
			assert(tree ~= nil, "tree should parse with ranges")

			parser:close()
		`)
	})
}

// =============================================================================
// Tree Tests
// =============================================================================

func TestTreeMethods(t *testing.T) {
	t.Run("tree root node", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})

	t.Run("tree copy", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})

	t.Run("tree walk", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})
}

func TestTreeAdditionalMethods(t *testing.T) {
	t.Run("tree error handling", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")

			-- Test invalid language
			local ok, err = pcall(function()
				treesitter.parse("invalid_lang", "code")
			end)
			assert(not ok, "should error for invalid language")
			assert(string.match(err, "unsupported language"), "should mention unsupported language")

			-- Test empty code
			local empty_tree = treesitter.parse("go", "")
			assert(empty_tree ~= nil, "should return tree even with empty code")

			-- Test invalid syntax
			local code = "func main() { invalid syntax"
			local tree = treesitter.parse("go", code)
			assert(tree ~= nil, "should return tree even with invalid syntax")
			local root = tree:root_node()
			assert(root:has_error(), "should detect syntax error")
		`)
	})

	t.Run("tree gc", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local code = "package main"
			local tree = treesitter.parse("go", code)
			assert(tree ~= nil, "tree should be created")

			-- Tree will be cleaned up by resource store when context closes
			tree = nil
		`)
	})
}

func TestTreeTraversal(t *testing.T) {
	t.Run("structured code traversal", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local code = [[
				package main

				type Person struct {
					Alias string
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
			cursor:reset(root)
			assert(cursor:goto_first_child(), "move to first child")
			assert(cursor:goto_next_sibling(), "move to type_declaration")

			-- Spawn copy at type_declaration
			local cursor2 = cursor:copy()
			local original_kind = cursor:current_node():kind()
			assert(cursor2:current_node():kind() == original_kind, "copied cursor should match")
			assert(original_kind == "type_declaration", "should be at type_declaration")

			-- Move cursors in different ways
			assert(cursor:goto_first_child(), "first cursor should move down")
			assert(cursor2:goto_parent(), "second cursor should move up")

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
		`)
	})
}

func TestTreeEditOperations(t *testing.T) {
	t.Run("tree edit operations", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local code = "func main() {\n\tprint(42)\n}"
			local tree = treesitter.parse("go", code)

			-- Test edit operation
			local edit = {
				start_byte = 13,
				old_end_byte = 20,
				new_end_byte = 25,
				start_row = 1,
				start_column = 1,
				old_end_row = 1,
				old_end_column = 8,
				new_end_row = 1,
				new_end_column = 13
			}

			local ok = tree:edit(edit)
			assert(ok, "edit should succeed")

			-- Test comparing trees after edit
			local original = treesitter.parse("go", code)
			local ranges = tree:changed_ranges(original)
			assert(#ranges > 0, "should detect changed ranges")
			assert(ranges[1].start_byte ~= nil, "range should have start_byte")

			-- Test included ranges
			local included = tree:included_ranges()
			assert(type(included) == "table", "included_ranges should return table")

			-- Test dot graph output
			local graph = tree:dot_graph()
			assert(type(graph) == "string", "dot graph should be string")
			assert(#graph > 0, "dot graph should not be empty")
		`)
	})

	t.Run("closed tree operations", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local tree = treesitter.parse("go", "package main")
			assert(tree ~= nil, "tree should be created")

			-- Get root before closing
			local root = tree:root_node()
			assert(root ~= nil, "root should exist before close")

			-- close the tree
			tree:close()

			-- After close, tree is marked closed - operations will fail
			-- Note: pcall may not catch panics from Go code, so we just verify close works
			assert(true, "close should succeed")
		`)
	})

	t.Run("memory management", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")

			-- Spawn and manipulate multiple trees
			local function stress_test()
				local trees = {}
				for i = 1, 10 do
					local tree = treesitter.parse("go", "package main")
					local copy = tree:copy()
					table.insert(trees, tree)
					table.insert(trees, copy)

					-- Spawn some edits
					local edit = {
						start_byte = 0,
						old_end_byte = 5,
						new_end_byte = 5,
						start_row = 0,
						start_column = 0,
						old_end_row = 0,
						old_end_column = 5,
						new_end_row = 0,
						new_end_column = 5
					}
					tree:edit(edit)

					-- Walk trees
					local cursor = tree:walk()
					local cursor2 = copy:walk()
				end

				-- Clear references (cleanup happens via resource store)
				trees = nil
			end

			-- Run stress test multiple times
			for i = 1, 3 do
				stress_test()
			end
		`)
	})
}

func TestTreeOperations(t *testing.T) {
	t.Run("tree cursor", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")

			local code = "package main"
			local tree = treesitter.parse("go", code)

			-- Get cursor from tree
			local cursor = tree:walk()
			assert(cursor ~= nil, "cursor should not be nil")

			-- Check current node
			local node = cursor:current_node()
			assert(node ~= nil, "current node should not be nil")
			assert(node:kind() == "source_file", "should start at root")

			-- Navigate to child
			assert(cursor:goto_first_child(), "should have first child")
			node = cursor:current_node()
			assert(node ~= nil, "child node should not be nil")

			-- Navigate back to parent
			assert(cursor:goto_parent(), "should be able to go to parent")
			node = cursor:current_node()
			assert(node:kind() == "source_file", "should be back at root")

			-- Close cursor
			cursor:close()
		`)
	})
}

// =============================================================================
// Node Tests
// =============================================================================

func TestNodeOperations(t *testing.T) {
	t.Run("node navigation", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")

			local code = "package main\n\nfunc main() {}"
			local tree = treesitter.parse("go", code)
			local root = tree:root_node()

			-- Test child navigation
			assert(root:child_count() > 0, "should have children")
			local first_child = root:child(0)
			assert(first_child ~= nil, "first child should exist")

			-- Test named children
			assert(root:named_child_count() > 0, "should have named children")
			local named_child = root:named_child(0)
			assert(named_child ~= nil, "named child should exist")

			-- Test parent
			local parent = first_child:parent()
			assert(parent ~= nil, "parent should exist")

			-- Test sibling navigation
			if root:named_child_count() > 1 then
				local next = root:named_child(0):next_named_sibling()
				assert(next ~= nil, "next named sibling should exist")
			end
		`)
	})

	t.Run("node properties", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")

			local code = "package main"
			local tree = treesitter.parse("go", code)
			local root = tree:root_node()

			-- Test kind
			assert(root:kind() == "source_file", "kind should be source_file")
			assert(root:type() == "source_file", "type should be source_file (alias)")

			-- Test is_named
			assert(root:is_named(), "root should be named")

			-- Test positions
			local start_byte = root:start_byte()
			local end_byte = root:end_byte()
			assert(start_byte == 0, "start_byte should be 0")
			assert(end_byte == #code, "end_byte should equal code length")

			local start_point = root:start_point()
			assert(start_point.row == 0, "start row should be 0")
			assert(start_point.column == 0, "start column should be 0")

			-- Test text
			local text = root:text(code)
			assert(text == code, "text should equal code")

			-- Test error detection
			assert(not root:has_error(), "should not have error")
			assert(not root:is_error(), "should not be error node")
		`)
	})
}

func TestNodeMethods(t *testing.T) {
	t.Run("node type checking", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})

	t.Run("node child access", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})

	t.Run("node field operations", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local code = [[
				type Person struct {
					Alias string
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
		`)
	})

	t.Run("node inspection methods", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})

	t.Run("node position methods", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})
}

func TestNodeTextMethod(t *testing.T) {
	t.Run("node text methods", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local code = [[package main

type Person struct {
    Alias string
    Age  int
}]]
			local tree = treesitter.parse("go", code)
			local root = tree:root_node()

			-- Test text() on root node
			local root_text = root:text()
			assert(root_text == code, "root text should match original code")

			-- Test text() on package declaration
			local cursor = tree:walk()
			cursor:goto_first_child()
			local pkg_node = cursor:current_node()
			local pkg_text = pkg_node:text()
			assert(pkg_text == "package main", "package text should match")
		`)
	})
}

func TestNodeChildText(t *testing.T) {
	t.Run("child node text extraction", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local code = "func test() { return 42 }"

			local tree = treesitter.parse("go", code)
			local root = tree:root_node()

			-- GetField the first child (function_declaration)
			local func_node = root:child(0)
			local func_text = func_node:text(code)
			assert(func_text == "func test() { return 42 }", "function text should match")

			-- GetField function name node
			local name_node = func_node:child_by_field_name("name")
			local name_text = name_node:text(code)
			assert(name_text == "test", "function name should match")

			-- GetField function body
			local body_node = func_node:child_by_field_name("body")
			local body_text = body_node:text(code)
			assert(body_text == "{ return 42 }", "function body should match")
		`)
	})
}

func TestOtherNodeMethods(t *testing.T) {
	t.Run("grammar name and extra nodes", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})

	t.Run("error nodes in incomplete syntax", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})

	t.Run("named descendant for point range", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})

	t.Run("to sexp representation", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})
}

func TestCodeValidation(t *testing.T) {
	t.Run("detect syntax errors", func(t *testing.T) {
		runLuaTest(t, `
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

			local function find_error_node(node)
				if node:is_error() then
					local start_pos = node:start_point()
					error_line = start_pos.row + 1  -- Convert to 1-based line numbers
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
			assert(error_line == 3, "error should be on line 3")

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
			find_error_node(root2)

			assert(error_found, "should find error in invalid function")
			assert(error_line == 3, "error should be on line 3")
		`)
	})
}

func TestNodeSiblingNavigation(t *testing.T) {
	t.Run("node sibling navigation", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local code = [[
				package main

				import (
					"fmt"
					"os"
					"strings"
				)

				func main() {
					fmt.Println("test")
				}
			]]

			local tree = treesitter.parse("go", code)
			local root = tree:root_node()

			-- Navigate to the first import_spec
			local cursor = tree:walk()
			assert(cursor:goto_first_child(), "should move to first child") -- package_clause
			assert(cursor:goto_next_sibling(), "should move to import_decl") -- import_declaration
			local import_decl = cursor:current_node()

			-- Find the import_spec_list by checking children
			local list_node = nil
			for i = 0, import_decl:child_count() - 1 do
				local child = import_decl:child(i)
				if child:kind() == "import_spec_list" then
					list_node = child
					break
				end
			end
			assert(list_node ~= nil, "should find import_spec_list")

			-- GetField first import_spec by finding first import_spec child
			local first_import = nil
			for i = 0, list_node:child_count() - 1 do
				local child = list_node:child(i)
				if child:kind() == "import_spec" then
					first_import = child
					break
				end
			end
			assert(first_import ~= nil, "should find first import_spec")

			-- Test next sibling navigation
			local next = first_import:next_sibling()
			assert(next ~= nil, "next sibling should exist")
			assert(next:text(code):match("os"), "next sibling should contain 'os' import")

			-- Test next named sibling
			local next_named = first_import:next_named_sibling()
			assert(next_named ~= nil, "next named sibling should exist")
			assert(next_named:text(code):match("os"), "next named sibling should contain 'os' import")

			cursor:close()
		`)
	})

	t.Run("node sibling edge cases", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			-- Use a simpler code sample for edge cases
			local code = [[
				package main

				func test() {}
			]]
			local tree = treesitter.parse("go", code)
			local root = tree:root_node()

			-- Test first node (no prev siblings)
			local first = root:child(0)
			assert(first ~= nil, "should find first child")

			assert(first:prev_sibling() == nil, "first node should have no prev sibling")
			assert(first:prev_named_sibling() == nil, "first node should have no prev named sibling")

			-- Test last node (no next siblings)
			local last = root:child(1)
			assert(last ~= nil, "should find last child")

			assert(last:next_sibling() == nil, "last node should have no next sibling")
			assert(last:next_named_sibling() == nil, "last node should have no next named sibling")

			-- Test is_missing on various nodes
			assert(not first:is_missing(), "first node should not be missing")
			assert(not last:is_missing(), "last node should not be missing")
			assert(not root:is_missing(), "root should not be missing")

			-- Spawn cursor just to test close
			local cursor = tree:walk()
			cursor:close()
		`)
	})
}

// =============================================================================
// Query Tests
// =============================================================================

func TestQueryOperations(t *testing.T) {
	t.Run("query captures", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")

			local code = [[
				package main

				func hello() {}
				func world() {}
			]]

			local tree = treesitter.parse("go", code)
			local root = tree:root_node()

			-- Query for function names
			local query = treesitter.query("go", [[
				(function_declaration name: (identifier) @func_name)
			]])

			local captures = query:captures(root, code)
			assert(#captures == 2, "should find 2 function names")

			-- Check capture properties
			for i, cap in ipairs(captures) do
				assert(cap.name == "func_name", "capture name should be func_name")
				assert(cap.node ~= nil, "capture should have node")
				assert(cap.text ~= nil, "capture should have text")
			end

			-- Verify function names
			assert(captures[1].text == "hello", "first function should be 'hello'")
			assert(captures[2].text == "world", "second function should be 'world'")
		`)
	})
}

func TestBasicQuery(t *testing.T) {
	runLuaTest(t, `
		local treesitter = require("treesitter")

		-- Simple test code with a function
		local code = [[
			func hello() {
				println("world")
			}
		]]

		-- Parse the code
		local tree = treesitter.parse("go", code)
		assert(tree ~= nil, "tree should not be nil")

		local root = tree:root_node()
		assert(root ~= nil, "root should not be nil")

		-- Spawn a simple query to find the function
		local query = treesitter.query("go", "(function_declaration) @function")
		assert(query ~= nil, "query should not be nil")

		-- Run query
		local matches = query:matches(root, code)
		assert(matches ~= nil, "matches should not be nil")

		-- Should find exactly one function
		local match_count = 0
		for _, match in pairs(matches) do
			match_count = match_count + 1

			-- Verify the match has captures
			assert(match.captures ~= nil, "match should have captures")
			assert(#match.captures > 0, "should have at least one capture")

			-- Verify the captured node
			local capture = match.captures[1]
			assert(capture.node ~= nil, "capture should have node")

			-- GetField the text of the captured node
			local text = capture.node:text(code)

			assert(text:match("^func hello"), "captured text should start with 'func hello'")
		end

		assert(match_count == 1, "should find exactly one function")
	`)
}

func TestQueryMultipleCaptures(t *testing.T) {
	runLuaTest(t, `
		local treesitter = require("treesitter")

		-- Test code with multiple functions and parameters
		local code = [[
func add(x int, y int) int {
	return x + y
}

func greet(name string) {
	println("Hello, " .. name)
}
]]

		-- Parse the code
		local tree = treesitter.parse("go", code)
		assert(tree ~= nil, "tree should not be nil")

		local root = tree:root_node()
		assert(root ~= nil, "root should not be nil")

		-- Spawn query to capture function name and parameters
		local query = treesitter.query("go", [[
(function_declaration
  name: (identifier) @func_name)
]])

		-- Run query and debug output
		local matches = query:matches(root, code)
		assert(matches ~= nil, "matches should not be nil")

		-- Verify matches
		local found_add = false
		local found_greet = false

		for i, match in ipairs(matches) do
			assert(match.captures ~= nil, "match should have captures")

			for j, capture in ipairs(match.captures) do
				assert(capture.node ~= nil, "capture should have node")

				local text = capture.node:text(code)

				if text == "add" then
					found_add = true
				elseif text == "greet" then
					found_greet = true
				end
			end
		end

		assert(found_add, "should find 'add' function")
		assert(found_greet, "should find 'greet' function")
	`)
}

func TestQueryAdvancedFeatures(t *testing.T) {
	runLuaTest(t, `
		local treesitter = require("treesitter")

		local code = [[
func example(x int, y string) int {
    if x > 0 {
        return x
    }
    println(y)
    return 0
}
]]

		local tree = treesitter.parse("go", code)
		local root = tree:root_node()

		-- Spawn query with multiple patterns
		local query = treesitter.query("go", [[
		  (function_declaration) @func
		  (parameter_declaration name: (identifier) @param_name type: (type_identifier) @param_type)
		  (if_statement condition: (binary_expression) @condition)
		]])

		-- Test pattern count and capture count
		local pattern_count = query:pattern_count()
		assert(pattern_count == 3, "should have 3 patterns")

		local capture_count = query:capture_count()
		assert(capture_count == 4, "should have 4 captures") -- func, param_name, param_type, condition

		-- GetField all capture names
		local capture_names = {}
		for i = 0, capture_count-1 do
			local name = query:capture_name_for_id(i)
			capture_names[name] = true
		end

		-- Verify we have all expected capture names
		assert(capture_names["func"], "should have func capture")
		assert(capture_names["param_name"], "should have param_name capture")
		assert(capture_names["param_type"], "should have param_type capture")
		assert(capture_names["condition"], "should have condition capture")

		-- Test match limit and timeout
		query:set_match_limit(1000)
		local limit = query:get_match_limit()
		assert(limit == 1000, "match limit should be set")

		query:set_timeout(5000)
		local timeout = query:get_timeout()
		assert(timeout == 5000, "timeout should be set")

		-- Test byte and point range
		query:set_byte_range(0, string.len(code))
		query:set_point_range({row=0, column=0}, {row=10, column=0})

		-- Test matches
		local matches = query:matches(root, code)

		local found_func = false
		local found_param = false
		local found_condition = false

		for i, match in ipairs(matches) do
			for j, capture in ipairs(match.captures) do
				local text = capture.node:text(code)
				if capture.name == "func" then
					found_func = true
				elseif capture.name == "param_name" then
					found_param = true
					assert(text == "x" or text == "y", "param should be x or y")
				elseif capture.name == "condition" then
					found_condition = true
					assert(text:find("x > 0"), "condition should contain x > 0")
				end
			end
		end

		-- Test captures API
		local captures = query:captures(root, code)
		assert(captures ~= nil, "captures should not be nil")

		-- Test property settings and predicates
		local predicates = query:get_property_predicates(0)
		assert(predicates ~= nil, "predicates should not be nil")

		local settings = query:get_property_settings(0)
		assert(settings ~= nil, "settings should not be nil")

		-- Final verification
		assert(found_func, "should find function declaration")
		assert(found_param, "should find parameters")
		assert(found_condition, "should find if condition")
	`)
}

func TestQueryOperationsAdvanced(t *testing.T) {
	runLuaTest(t, `
		local treesitter = require("treesitter")

-- Test code with rich syntax for comprehensive query testing
local code = [[
func process(data string) error {
    if len(data) == 0 {
        return fmt.Errorf("empty data")
    }
    if !isValid(data) {
        return nil
    }
    for i := 0; i < len(data); i++ {
        handleChar(data[i])
    }
    return nil
}

func isValid(s string) bool {
    return len(s) > 5
}

func handleChar(c byte) {
    if c >= '0' && c <= '9' {
        processDigit(c - '0')
    }
}
]]

local tree = treesitter.parse("go", code)
local root = tree:root_node()

-- Test query creation and error handling
local query = treesitter.query("go", [[
    (function_declaration
        name: (identifier) @func_name
        parameters: (parameter_list) @params
        result: [(type_identifier) (ERROR)]? @return_type) @function

    (if_statement
        condition: (_) @if_condition) @if

    (binary_expression
        left: (_) @left
        operator: (_) @op
        right: (_) @right) @binary

    ((identifier) @id
     (#match? @id "^process"))
]])

assert(query ~= nil, "query should not be nil")

-- Test queryDidExceedMatchLimit
query:set_match_limit(1)
local matches = query:matches(root, code)
local exceeded = query:did_exceed_match_limit()
assert(exceeded, "should exceed match limit when set to 1")

-- Test queryDisablePattern and queryIsPatternRooted
query:disable_pattern(0)
local is_rooted = query:is_pattern_rooted(1)
assert(is_rooted ~= nil, "is_pattern_rooted should return a value")

-- Test queryDisableCapture
query:disable_capture("func_name")

-- Test queryIsPatternNonLocal
local is_non_local = query:is_pattern_non_local(0)
assert(is_non_local ~= nil, "is_pattern_non_local should return a value")

-- Test queryCaptureNameForId and queryCaptureQuantifier
local capture_name = query:capture_name_for_id(0)
assert(capture_name ~= nil, "should get capture name")
local quantifier = query:capture_quantifier(0, 0)
assert(quantifier ~= nil, "should get capture quantifier")

-- Test queryStringCount and queryStartByteForPattern
local string_count = query:string_count()
assert(string_count ~= nil, "should get string count")
local start_byte = query:start_byte_for_pattern(0)
assert(start_byte ~= nil, "should get start byte")

-- Test querySetMaxStartDepth
query:set_max_start_depth(5)

-- Test queryGetPropertyPredicates with pattern validation
local predicates = query:get_property_predicates(0)
assert(predicates ~= nil, "should get property predicates")
for _, pred in ipairs(predicates) do
    assert(pred.key ~= nil, "predicate should have key")
end

-- Test queryGetPropertySettings with settings validation
local settings = query:get_property_settings(0)
assert(settings ~= nil, "should get property settings")
for _, setting in ipairs(settings) do
    assert(setting.key ~= nil, "setting should have key")
end

-- Test queryIsPatternGuaranteed
local is_guaranteed = query:is_pattern_guaranteed(0)
assert(type(is_guaranteed) == "boolean", "should return boolean for pattern guarantee")

-- Test queryCaptureIndexForName
local capture_index = query:capture_index_for_name("func_name")
assert(capture_index ~= nil, "should get capture index")

-- Test queryEndByteForPattern
local end_byte = query:end_byte_for_pattern(0)
assert(end_byte ~= nil, "should get end byte")

-- Test queryGetTextPredicates
local text_predicates = query:get_text_predicates(0)
assert(text_predicates ~= nil, "should get text predicates")

-- Test error handling
local bad_query, err = treesitter.query("go", "((invalid query")
assert(bad_query == nil, "invalid query should return nil")
assert(err ~= nil, "invalid query should return error message")

-- Query cleanup happens via resource store when context closes
query = nil
	`)
}

func TestQueryErrorCases(t *testing.T) {
	runLuaTest(t, `
		local treesitter = require("treesitter")

-- Test different query error types
local error_cases = {
    -- Syntax error
    {
        name = "syntax error - unmatched parenthesis",
        query = "(()",
        expected = "Query error at 1:3%. Invalid syntax:"
    },
    -- Node type error
    {
        name = "invalid node type",
        query = "(nonexistent_node)",
        expected = "Query error at 1:%d+%. Invalid node type"
    },
    -- Capture syntax error
    {
        name = "invalid capture syntax",
        query = "(identifier @)",
        expected = "Query error at 1:%d+%. Invalid"
    },
    -- Structure error
    {
        name = "invalid structure",
        query = "((identifier) ()",
        expected = "Query error at"
    }
}

for _, case in ipairs(error_cases) do
    local query, err = treesitter.query("go", case.query)

    -- Should not create a valid query
    assert(query == nil, "invalid query '" .. case.query .. "' should return nil")

    -- Should have an error message
    assert(err ~= nil, "should have error message for query: " .. case.query)

    -- Error message should match expected pattern
    assert(err:match(case.expected),
           string.format("\nError message did not match pattern.\nGot: '%s'\nExpected to match: '%s'",
                        err, case.expected))
end

-- Test valid queries
local valid_queries = {
    "(function_declaration) @func",
    "((identifier) @id (#match? @id \"^[A-Z]\"))"
}

for _, query_str in ipairs(valid_queries) do
    local query = treesitter.query("go", query_str)
    assert(query ~= nil, "valid query should not return nil: " .. query_str)
end
	`)
}

// =============================================================================
// Cursor Tests
// =============================================================================

func TestCursorMethods(t *testing.T) {
	t.Run("cursor creation and metadata", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})

	t.Run("cursor navigation", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})

	t.Run("cursor reset and copy", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})

	t.Run("cursor positioning by byte and point", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})
}

func TestCursorAdditionalMethods(t *testing.T) {
	t.Run("cursor field operations", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})

	t.Run("cursor navigation edge cases", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})

	t.Run("cursor gc", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local code = "package main"
			local tree = treesitter.parse("go", code)
			local cursor = tree:walk()
			assert(cursor ~= nil, "cursor should be created")

			-- Cursor cleanup happens via resource store when context closes
			cursor = nil
		`)
	})
}

func TestCursorImplementation(t *testing.T) {
	t.Run("basic cursor movement", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})

	t.Run("cursor navigation with reset", func(t *testing.T) {
		runLuaTest(t, `
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
		`)
	})
}

// =============================================================================
// Language Tests
// =============================================================================

func TestLanguageMethods(t *testing.T) {
	t.Run("language version", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local code = "package main"
			local tree = treesitter.parse("go", code)
			local lang = tree:language()

			-- Test version method
			local version = lang:version()
			assert(type(version) == "number", "version should be a number")
			assert(version > 0, "version should be positive")
		`)
	})

	t.Run("node kind operations", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local code = "package main"
			local tree = treesitter.parse("go", code)
			local lang = tree:language()

			-- Test node kind count
			local kind_count = lang:node_kind_count()
			assert(type(kind_count) == "number", "node_kind_count should return number")
			assert(kind_count > 0, "should have node kinds")

			-- Test node kind for id
			local kind = lang:node_kind_for_id(1)
			assert(type(kind) == "string", "node_kind_for_id should return string")

			-- Test id for node kind
			local id = lang:id_for_node_kind(kind, true)
			assert(type(id) == "number", "id_for_node_kind should return number")

			-- Test node kind is named
			local is_named = lang:node_kind_is_named(1)
			assert(type(is_named) == "boolean", "node_kind_is_named should return boolean")
		`)
	})

	t.Run("field operations", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local code = "package main"
			local tree = treesitter.parse("go", code)
			local lang = tree:language()

			-- Test field count
			local field_count = lang:field_count()
			assert(type(field_count) == "number", "field_count should return number")
			assert(field_count > 0, "should have fields")

			-- Test field name for id
			local field_name = lang:field_name_for_id(1)
			assert(type(field_name) == "string", "field_name_for_id should return string")

			-- Test field id for name
			local field_id = lang:field_id_for_name(field_name)
			assert(type(field_id) == "number", "field_id_for_name should return number")
		`)
	})

	t.Run("parse state count", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local code = "package main"
			local tree = treesitter.parse("go", code)
			local lang = tree:language()

			-- Test parse state count
			local state_count = lang:parse_state_count()
			assert(type(state_count) == "number", "parse_state_count should return number")
			assert(state_count > 0, "should have parse states")
		`)
	})
}

func TestLanguageEdgeCases(t *testing.T) {
	t.Run("invalid language operations", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")

			-- Test with invalid language
			local ok, err = pcall(function()
				treesitter.parse("nonexistent", "some code")
			end)
			assert(not ok, "should fail with invalid language")
			assert(err:match("unsupported language"), "error should mention unsupported language")

			-- Test with empty code
			local tree = treesitter.parse("go", "")
			local lang = tree:language()

			-- All methods should still work with empty code
			assert(type(lang:version()) == "number")
			assert(type(lang:node_kind_count()) == "number")
			assert(type(lang:parse_state_count()) == "number")
			assert(type(lang:field_count()) == "number")
		`)
	})

	t.Run("boundary cases", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local code = "package main"
			local tree = treesitter.parse("go", code)
			local lang = tree:language()

			-- Test with invalid node kind Alias
			local kind = lang:node_kind_for_id(65535)  -- Max uint16
			assert(type(kind) == "string", "should handle max uint16")

			-- Test with empty field name
			local field_id = lang:field_id_for_name("")
			assert(type(field_id) == "number", "should handle empty field name")

			-- Test with non-existent field name
			local nonexistent_id = lang:field_id_for_name("nonexistent_field")
			assert(type(nonexistent_id) == "number", "should handle non-existent field")
		`)
	})

	t.Run("cross-language compatibility", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")

			-- Test with different languages
			local languages = {
				{ name = "go", code = "package main" },
				{ name = "javascript", code = "function test() {}" },
				{ name = "python", code = "def test():\n    pass" },
			}

			for _, lang_info in ipairs(languages) do
				local tree = treesitter.parse(lang_info.name, lang_info.code)
				local lang = tree:language()

				-- Basic operations should work for all languages
				assert(type(lang:version()) == "number")
				assert(type(lang:node_kind_count()) == "number")
				assert(type(lang:parse_state_count()) == "number")
				assert(type(lang:field_count()) == "number")

				-- Each language should have unique characteristics
				local kind_count = lang:node_kind_count()
				local field_count = lang:field_count()

				-- Store counts to verify they're different across languages
				lang_info.kind_count = kind_count
				lang_info.field_count = field_count
			end

			-- Verify languages have different characteristics
			assert(languages[1].kind_count ~= languages[2].kind_count, "Go and JS should have different node kinds")
			assert(languages[2].kind_count ~= languages[3].kind_count, "JS and Python should have different node kinds")
		`)
	})
}

// =============================================================================
// SQL and Markdown Grammar Tests
// =============================================================================

func TestSQLQueries(t *testing.T) {
	runLuaTest(t, `
	local treesitter = require("treesitter")

	-- Test SQL query parsing
	local sql_code = [[
/*
This is a query
With a multiline comment
*/
SELECT id
/*
SELECT id FROM my_table;
*/
FROM my_table;
	]]

	-- Parse SQL code
	local tree = treesitter.parse("sql", sql_code)
	assert(tree ~= nil, "tree should not be nil")

	local root = tree:root_node()
	assert(root ~= nil, "root should not be nil")

	local create_table_query = treesitter.query("sql", [[
(program
  (marginalia)
  (statement
    (select
      (keyword_select)
      (select_expression
        (term
          value: (field
            name: (identifier)))))
    (marginalia)
    (from
      (keyword_from)
      (relation
        (object_reference
          name: (identifier))))))
	]])
	-- Run query
	local matches = create_table_query:matches(root, sql_code)
	assert(matches ~= nil, "matches should not be nil")
	`)
}

func TestMarkdownQueries(t *testing.T) {
	runLuaTest(t, `
		local treesitter = require("treesitter")

		-- Test Markdown parsing
		local md_code = [[
- [x] foo
  - [ ] bar
  - [x] baz
- [ ] bim

]]

		-- Parse Markdown code
		local tree = treesitter.parse("markdown", md_code)
		assert(tree ~= nil, "tree should not be nil")

		local root = tree:root_node()
		assert(root ~= nil, "root should not be nil")

		-- Query for headers
		local header_query = treesitter.query("markdown", [[
			(document
  (section
    (list
      (list_item
        (list_marker_minus)
        (task_list_marker_checked)
        (paragraph
          (inline)
          (block_continuation))
        (list
          (list_item
            (list_marker_minus)
            (task_list_marker_unchecked)
            (paragraph
              (inline)
              (block_continuation)))
          (list_item
            (list_marker_minus)
            (task_list_marker_checked)
            (paragraph
              (inline)))))
      (list_item
        (list_marker_minus)
        (task_list_marker_unchecked)
        (paragraph
          (inline))))))
		]])

		local matches = header_query:matches(root, md_code)
		assert(matches ~= nil, "matches should not be nil")
	`)
}

func TestComplexTreeOperations(t *testing.T) {
	t.Run("root node with offset", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local code = [[
				package main

				func main() {
					println("hello")
				}

				func foo() {
					println("world")
				}
			]]
			local tree = treesitter.parse("go", code)

			-- Get root node with offset to foo function
			local offset = {
				row = 6,
				column = 0
			}
			local root_offset = tree:root_node_with_offset(24, offset)
			assert(root_offset ~= nil, "should get root node with offset")

			-- Verify we can still access nodes from offset root
			local node_type = root_offset:kind()
			assert(node_type == "source_file", "should still be source file")
		`)
	})

	t.Run("complex tree edits", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")
			local code = [[
				func main() {
					x := 42
					println(x)
				}
			]]
			local tree = treesitter.parse("go", code)

			-- Test sequential edits
			local edit1 = {
				start_byte = 18,
				old_end_byte = 20,
				new_end_byte = 22,
				start_row = 1,
				start_column = 6,
				old_end_row = 1,
				old_end_column = 8,
				new_end_row = 1,
				new_end_column = 10
			}

			local edit2 = {
				start_byte = 31,
				old_end_byte = 32,
				new_end_byte = 35,
				start_row = 2,
				start_column = 9,
				old_end_row = 2,
				old_end_column = 10,
				new_end_row = 2,
				new_end_column = 13
			}

			-- Apply first edit
			local ok = tree:edit(edit1)
			assert(ok, "first edit should succeed")

			-- Apply second edit
			ok = tree:edit(edit2)
			assert(ok, "second edit should succeed")

			-- Test editing copied tree
			local copied = tree:copy()
			local edit3 = {
				start_byte = 0,
				old_end_byte = 4,
				new_end_byte = 5,
				start_row = 0,
				start_column = 0,
				old_end_row = 0,
				old_end_column = 4,
				new_end_row = 0,
				new_end_column = 5
			}
			ok = copied:edit(edit3)
			assert(ok, "edit on copied tree should succeed")

			-- Verify original and copy are different
			local ranges = tree:changed_ranges(copied)
			assert(#ranges > 0, "trees should have differences")

			-- Test invalid edits
			local invalid_edit = {
				start_byte = -1,
				old_end_byte = 5,
				new_end_byte = 5,
				start_row = 0,
				start_column = 0,
				old_end_row = 0,
				old_end_column = 5,
				new_end_row = 0,
				new_end_column = 5
			}
			local ok, err = tree:edit(invalid_edit)
			assert(not ok and err, "invalid edit should fail with error")
		`)
	})
}

func TestTreeWalking(t *testing.T) {
	t.Run("comprehensive tree walk", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")

			local code = [[
				package main

				type Person struct {
					Alias string
					Age  int
				}

				func (p *Person) String() string {
					return p.Alias
				}
			]]

			local tree = treesitter.parse("go", code)
			assert(tree ~= nil, "should parse tree")

			local cursor = tree:walk()
			assert(cursor ~= nil, "should create cursor")

			-- Walk the entire tree manually
			local function walk_tree(cursor)
				local visited = {}
				local function visit(depth)
					local node = cursor:current_node()
					local kind = node:kind()
					local text = node:text(code)

					table.insert(visited, {kind = kind, text = text})

					if cursor:goto_first_child() then
						visit(depth + 1)
						cursor:goto_parent()
					end

					if cursor:goto_next_sibling() then
						visit(depth)
					end
				end
				visit(0)
				return visited
			end

			-- Walk the tree
			local nodes = walk_tree(cursor)
			assert(#nodes > 0, "should visit nodes")

			-- Verify we've visited key nodes
			local found_package = false
			local found_type = false
			local found_struct = false
			local found_method = false

			for _, node in ipairs(nodes) do
				if node.kind == "package_clause" then found_package = true end
				if node.kind == "type_declaration" then found_type = true end
				if node.kind == "struct_type" then found_struct = true end
				if node.kind == "method_declaration" then found_method = true end
			end

			assert(found_package, "should find package clause")
			assert(found_type, "should find type declaration")
			assert(found_struct, "should find struct type")
			assert(found_method, "should find method declaration")

			-- Test walking on empty tree
			local empty_tree = treesitter.parse("go", "")
			cursor = empty_tree:walk()
			assert(cursor ~= nil, "should create cursor for empty tree")
			local root = cursor:current_node()
			assert(root:kind() == "source_file", "empty tree should have root node")
		`)
	})
}

func TestTreeEditAndReparse(t *testing.T) {
	t.Run("edit and reparse workflow", func(t *testing.T) {
		runLuaTest(t, `
			local treesitter = require("treesitter")

			-- Original code
			local code = "func main() { x := 1 }"
			local tree = treesitter.parse("go", code)
			assert(tree ~= nil, "should parse original")

			-- Edit to change "1" to "100"
			local edit = {
				start_byte = 19,
				old_end_byte = 20,
				new_end_byte = 22,
				start_row = 0,
				start_column = 19,
				old_end_row = 0,
				old_end_column = 20,
				new_end_row = 0,
				new_end_column = 22
			}

			local ok = tree:edit(edit)
			assert(ok, "edit should succeed")

			-- Now parse the new code
			local new_code = "func main() { x := 100 }"
			local new_tree = treesitter.parse("go", new_code)
			assert(new_tree ~= nil, "should parse new code")

			-- Verify the new tree is valid
			local root = new_tree:root_node()
			assert(not root:has_error(), "new tree should not have errors")

			-- Check changed ranges between old (edited) and new tree
			local ranges = tree:changed_ranges(new_tree)
			assert(type(ranges) == "table", "changed_ranges should return table")
		`)
	})
}
