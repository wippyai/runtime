package treesitter

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestLanguageMethods(t *testing.T) {
	logger := zap.NewNop()

	t.Run("language version", func(t *testing.T) {
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
			local lang = tree:language()
			
			-- Test version method
			local version = lang:version()
			assert(type(version) == "number", "version should be a number")
			assert(version > 0, "version should be positive")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("language version", func(t *testing.T) {
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
			local lang = tree:language()
			
			-- Test version method
			local version = lang:version()
			assert(type(version) == "number", "version should be a number")
			assert(version > 0, "version should be positive")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("node kind operations", func(t *testing.T) {
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
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("field operations", func(t *testing.T) {
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
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("parse state count", func(t *testing.T) {
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
			local lang = tree:language()
			
			-- Test parse state count
			local state_count = lang:parse_state_count()
			assert(type(state_count) == "number", "parse_state_count should return number")
			assert(state_count > 0, "should have parse states")
		`, "test")
		assert.NoError(t, err)
	})
}

func TestLanguageEdgeCases(t *testing.T) {
	logger := zap.NewNop()

	t.Run("invalid language operations", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
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
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("boundary cases", func(t *testing.T) {
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
			local lang = tree:language()
			
			-- Test with invalid node kind Name
			local kind = lang:node_kind_for_id(65535)  -- Max uint16
			assert(type(kind) == "string", "should handle max uint16")
			
			-- Test with empty field name
			local field_id = lang:field_id_for_name("")
			assert(type(field_id) == "number", "should handle empty field name")
			
			-- Test with non-existent field name
			local nonexistent_id = lang:field_id_for_name("nonexistent_field")
			assert(type(nonexistent_id) == "number", "should handle non-existent field")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("cross-language compatibility", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
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
		`, "test")
		assert.NoError(t, err)
	})
}
