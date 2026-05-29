// SPDX-License-Identifier: MPL-2.0

//go:build treesitter

package treesitter

import (
	"context"
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestLoad(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	mod := l.GetGlobal("treesitter")
	if mod.Type() != lua.LTTable {
		t.Fatal("module not registered")
	}

	modTbl := mod.(*lua.LTable)
	if modTbl.RawGetString("parse").Type() != lua.LTFunction {
		t.Error("parse function not registered")
	}
	if modTbl.RawGetString("parser").Type() != lua.LTFunction {
		t.Error("parser function not registered")
	}
	if modTbl.RawGetString("query").Type() != lua.LTFunction {
		t.Error("query function not registered")
	}
	if modTbl.RawGetString("language").Type() != lua.LTFunction {
		t.Error("language function not registered")
	}
	if modTbl.RawGetString("supported_languages").Type() != lua.LTFunction {
		t.Error("supported_languages function not registered")
	}
}

func TestLoadReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	tbl, _ := Module.Build()
	l1.SetGlobal(Module.Name, tbl)
	l2.SetGlobal(Module.Name, tbl)

	mod1 := l1.GetGlobal("treesitter").(*lua.LTable)
	mod2 := l2.GetGlobal("treesitter").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestSupportedLanguages(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local langs = treesitter.supported_languages()
		if type(langs) ~= "table" then
			error("supported_languages should return a table")
		end
		if not langs["go"] then
			error("Go should be supported")
		end
		if not langs["javascript"] then
			error("JavaScript should be supported")
		end
		if not langs["python"] then
			error("Python should be supported")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestParse(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local tree = treesitter.parse("go", "package main")
		if tree == nil then
			error("tree should not be nil")
		end
		local root = tree:root_node()
		if root == nil then
			error("root should not be nil")
		end
		if root:kind() ~= "source_file" then
			error("root should be source_file, got: " .. root:kind())
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestParseInvalidLanguage(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local tree, err = treesitter.parse("nonexistent", "code")
		if tree ~= nil then
			error("tree should be nil for invalid language")
		end
		if err == nil then
			error("error should be returned")
		end
		if err:kind() ~= errors.INVALID then
			error("error kind should be INVALID, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestLanguage(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local go_lang = treesitter.language("go")
		if go_lang == nil then
			error("should get Go language")
		end
		local version = go_lang:version()
		if type(version) ~= "number" then
			error("version should be number")
		end
		if version <= 0 then
			error("version should be positive")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestLanguageInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local lang, err = treesitter.language("nonexistent")
		if lang ~= nil then
			error("lang should be nil for invalid language")
		end
		if err == nil then
			error("error should be returned")
		end
		if err:kind() ~= errors.INVALID then
			error("error kind should be INVALID")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestParser(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local parser = treesitter.parser()
		if parser == nil then
			error("parser should not be nil")
		end
		local ok = parser:set_language("go")
		if not ok then
			error("should set language successfully")
		end
		local lang = parser:get_language()
		if lang ~= "go" then
			error("language should be go")
		end
		local tree = parser:parse("package main")
		if tree == nil then
			error("tree should not be nil")
		end
		parser:close()
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestQuery(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local code = [[
			package main

			func hello() {}
			func world() {}
		]]
		local tree = treesitter.parse("go", code)
		local root = tree:root_node()

		local query = treesitter.query("go", [[
			(function_declaration name: (identifier) @func_name)
		]])
		if query == nil then
			error("query should not be nil")
		end

		local captures = query:captures(root, code)
		if #captures ~= 2 then
			error("should find 2 function names, got: " .. #captures)
		end
		if captures[1].text ~= "hello" then
			error("first function should be hello")
		end
		if captures[2].text ~= "world" then
			error("second function should be world")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestQueryInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local query, err = treesitter.query("go", "((invalid")
		if query ~= nil then
			error("query should be nil for invalid pattern")
		end
		if err == nil then
			error("error should be returned")
		end
		if err:kind() ~= errors.INVALID then
			error("error kind should be INVALID")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestTreeCursor(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local tree = treesitter.parse("go", "package main")
		local cursor = tree:walk()
		if cursor == nil then
			error("cursor should not be nil")
		end

		local node = cursor:current_node()
		if node == nil then
			error("current node should not be nil")
		end
		if node:kind() ~= "source_file" then
			error("should start at root")
		end

		local depth = cursor:current_depth()
		if depth ~= 0 then
			error("initial depth should be 0")
		end

		local ok = cursor:goto_first_child()
		if not ok then
			error("should have first child")
		end
		if cursor:current_depth() ~= 1 then
			error("depth should be 1 after going to child")
		end

		cursor:close()
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestNodeMethods(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local code = "package main"
		local tree = treesitter.parse("go", code)
		local root = tree:root_node()

		if root:kind() ~= "source_file" then
			error("kind should be source_file")
		end
		if root:type() ~= "source_file" then
			error("type should be source_file (alias)")
		end
		if not root:is_named() then
			error("root should be named")
		end
		if root:start_byte() ~= 0 then
			error("start_byte should be 0")
		end
		if root:end_byte() ~= #code then
			error("end_byte should equal code length")
		end
		if root:text() ~= code then
			error("text should equal code")
		end
		if root:has_error() then
			error("should not have error")
		end
		if root:child_count() == 0 then
			error("should have children")
		end

		local child = root:child(0)
		if child == nil then
			error("first child should exist")
		end
		local parent = child:parent()
		if parent == nil then
			error("parent should exist")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestTreeEdit(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local code = "func main() { x := 1 }"
		local tree = treesitter.parse("go", code)

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
		if not ok then
			error("edit should succeed")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestTreeCopy(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local tree = treesitter.parse("go", "package main")
		local copy = tree:copy()
		if copy == nil then
			error("copied tree should not be nil")
		end
		local root1 = tree:root_node()
		local root2 = copy:root_node()
		if root1 == nil or root2 == nil then
			error("both roots should exist")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestLuaLanguage(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local code = [[
			function greet(name)
				return "Hello, " .. name .. "!"
			end
		]]
		local tree = treesitter.parse("lua", code)
		if tree == nil then
			error("tree should not be nil")
		end
		local root = tree:root_node()
		if root == nil then
			error("root should not be nil")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestDoubleClose(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		-- Test parser double close
		local parser = treesitter.parser()
		parser:set_language("go")
		local tree = parser:parse("package main")
		tree:close()
		parser:close()
		parser:close()  -- Should not crash

		-- Test tree double close
		local tree2 = treesitter.parse("go", "package main")
		tree2:close()
		tree2:close()  -- Should not crash

		-- Test query double close
		local query = treesitter.query("go", "(identifier) @id")
		query:close()
		query:close()  -- Should not crash

		-- Test cursor double close
		local tree3 = treesitter.parse("go", "package main")
		local cursor = tree3:walk()
		cursor:close()
		cursor:close()  -- Should not crash
		tree3:close()
	`)
	if err != nil {
		t.Errorf("double close test failed: %v", err)
	}
}

func TestAllLanguages(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	testCases := []struct {
		lang string
		code string
		root string
	}{
		{"go", "package main", "source_file"},
		{"javascript", "const x = 1;", "program"},
		{"typescript", "const x: number = 1;", "program"},
		{"tsx", "const x = <div/>;", "program"},
		{"python", "x = 1", "module"},
		{"lua", "local x = 1", "chunk"},
		{"php", "<?php $x = 1; ?>", "program"},
		{"csharp", "class Foo {}", "compilation_unit"},
		{"html", "<div></div>", "document"},
		{"markdown", "# Hello\n\nWorld", "document"},
		{"sql", "SELECT 1", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.lang, func(t *testing.T) {
			l.SetTop(0)
			l.SetGlobal("test_lang", lua.LString(tc.lang))
			l.SetGlobal("test_code", lua.LString(tc.code))
			l.SetGlobal("expected_root", lua.LString(tc.root))

			err := l.DoString(`
				local tree = treesitter.parse(test_lang, test_code)
				if tree == nil then
					error("failed to parse " .. test_lang)
				end
				local root = tree:root_node()
				if root == nil then
					error("no root node for " .. test_lang)
				end
				if root:has_error() then
					error("parse error for " .. test_lang)
				end
				if expected_root ~= "" and root:kind() ~= expected_root then
					error("wrong root for " .. test_lang .. ": " .. root:kind())
				end
				tree:close()
			`)
			if err != nil {
				t.Errorf("language %s failed: %v", tc.lang, err)
			}
		})
	}
}

func TestParserTimeout(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local parser = treesitter.parser()
		parser:set_language("go")

		-- Test with string duration
		parser:set_timeout("1s")

		-- Test with nanoseconds
		parser:set_timeout(1000000000)

		local tree = parser:parse("package main")
		if tree == nil then
			error("tree should not be nil")
		end
		parser:close()
	`)
	if err != nil {
		t.Errorf("parser timeout test failed: %v", err)
	}
}

func TestQueryTimeout(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetContext(context.Background())
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local code = "package main\nfunc hello() {}"
		local tree = treesitter.parse("go", code)
		local root = tree:root_node()

		local query = treesitter.query("go", "(function_declaration) @fn")

		-- Test with string duration
		query:set_timeout("500ms")
		local timeout1 = query:get_timeout()
		if timeout1 ~= 500000000 then
			error("expected 500000000 nanoseconds, got " .. timeout1)
		end

		-- Test with nanoseconds
		query:set_timeout(1000000000)
		local timeout2 = query:get_timeout()
		if timeout2 ~= 1000000000 then
			error("expected 1000000000 nanoseconds, got " .. timeout2)
		end

		-- Query should still work
		local matches = query:matches(root, code)
		if #matches ~= 1 then
			error("expected 1 match, got " .. #matches)
		end

		query:close()
		tree:close()
	`)
	if err != nil {
		t.Errorf("query timeout test failed: %v", err)
	}
}
