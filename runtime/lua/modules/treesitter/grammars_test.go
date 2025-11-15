//go:build !windows

package treesitter

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	treesittergo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	treesitterjs "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"go.uber.org/zap"
)

func TestGetLanguageInfo(t *testing.T) {
	tests := []struct {
		name      string
		alias     string
		want      *LanguageInfo
		wantFound bool
	}{
		{
			name:  "GetField existing language (primary alias)",
			alias: "go",
			want: &LanguageInfo{
				Name:     "go",
				Aliases:  []string{"go", "golang"},
				Language: treesittergo.Language,
			},
			wantFound: true,
		},
		{
			name:  "GetField existing language (alternative alias)",
			alias: "js",
			want: &LanguageInfo{
				Name:     "javascript",
				Aliases:  []string{"js", "javascript"},
				Language: treesitterjs.Language,
			},
			wantFound: true,
		},
		{
			name:      "GetField non-existing language",
			alias:     "xyz",
			want:      nil,
			wantFound: false,
		},
		{
			name:  "GetField language with nil Language function",
			alias: "foo",
			want: &LanguageInfo{
				Name:     "bar",
				Aliases:  []string{"foobar", "bar"},
				Language: nil,
			},
			wantFound: false,
		},
	}

	langs := NewLanguages()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := langs.GetLanguageInfo(tt.alias)
			if (got != nil) != tt.wantFound {
				t.Errorf("GetLanguageInfo() found = %v, wantFound %v", got != nil, tt.wantFound)
				return
			}

			// If not found, we don't need to compare values
			if !tt.wantFound {
				return
			}

			// Compare field by field, skipping the Language function
			if got.Name != tt.want.Name {
				t.Errorf("GetLanguageInfo() Alias = %v, want %v", got.Name, tt.want.Name)
			}
			if !reflect.DeepEqual(got.Aliases, tt.want.Aliases) {
				t.Errorf("GetLanguageInfo() Aliases = %v, want %v", got.Aliases, tt.want.Aliases)
			}
			// For Language function, just check if both are nil or both are non-nil
			if (got.Language == nil) != (tt.want.Language == nil) {
				t.Errorf("GetLanguageInfo() Language presence = %v, want %v", got.Language == nil, tt.want.Language == nil)
			}
		})
	}
}

// TestLanguageFunctions checks if the Language functions return non-nil values.
func TestLanguageFunctions(t *testing.T) {
	langs := NewLanguages()
	for alias, info := range langs.supported {
		// Skip languages without a Language function (like Markdown)
		if info.Language == nil {
			continue
		}

		t.Run(alias, func(t *testing.T) {
			if got := info.Language(); got == nil {
				t.Errorf("Language() for '%s' returned nil, want non-nil", alias)
			}
		})
	}
}

func TestGrammarSupport(t *testing.T) {
	logger := zap.NewNop()

	t.Run("supported languages", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
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
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("language aliases", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
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
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("parser error handling", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
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
		`, "test")
		assert.NoError(t, err)
	})
}
