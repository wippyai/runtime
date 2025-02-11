package treesitter

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func assertLua(l *lua.LState) int {
	if l.ToBool(1) {
		return 0
	}
	l.RaiseError("%s", l.OptString(2, "assertion failed!"))
	return 0
}

func TestTreeSitterModule_Parse(t *testing.T) {
	logger := zap.NewNop()

	t.Run("basic parse", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local treesitter = require("treesitter")
			local code = "package main"
			local tree = treesitter.parse("go", code)
			assert(tree ~= nil, "tree should not be nil")
			assert(type(tree) == "userdata", "tree should be userdata")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestLanguageOperations(t *testing.T) {
	logger := zap.NewNop()

	t.Run("direct language operations", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
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
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("language edge cases", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local treesitter = require("treesitter")
			local lang = treesitter.language("go")
			
			-- Test with invalid node kind Alias
			local invalid_kind = lang:node_kind_for_id(65535)  -- max uint16
			assert(type(invalid_kind) == "string", "should handle max node kind id")
			
			-- Test with empty field name
			local field_id = lang:field_id_for_name("")
			assert(type(field_id) == "number", "should handle empty field name")
			
			-- Test with invalid field name
			local nonexistent_id = lang:field_id_for_name("nonexistent_field")
			assert(type(nonexistent_id) == "number", "should handle nonexistent field")
			
			-- Test node kind with empty name
			local empty_kind_id = lang:id_for_node_kind("", true)
			assert(type(empty_kind_id) == "number", "should handle empty node kind")
			
			-- Test named vs unnamed nodes
			local named_id = lang:id_for_node_kind("identifier", true)
			local unnamed_id = lang:id_for_node_kind("identifier", false)
			assert(named_id ~= unnamed_id, "named and unnamed ids should differ")
			
			-- Verify is_named behavior
			assert(type(lang:node_kind_is_named(named_id)) == "boolean",
				"should determine if node kind is named")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestLuaSupport(t *testing.T) {
	logger := zap.NewNop()
	mod := NewTreeSitterModule(logger)

	vm, err := engine.NewVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithGlobalFunction("assert", assertLua),
	)
	require.NoError(t, err)
	defer vm.Close()

	err = vm.DoString(context.Background(), `
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

		-- Create a simple query to find the function
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
	`, "test")
	assert.NoError(t, err)
}
