package treesitter

import (
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	treesittergo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	treesitterjs "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	"go.uber.org/zap"
	"reflect"
	"strings"
	"testing"
)

func TestGetLanguageInfo(t *testing.T) {
	tests := []struct {
		name      string
		alias     string
		want      *LanguageInfo
		wantFound bool
	}{
		{
			name:  "Get existing language (primary alias)",
			alias: "go",
			want: &LanguageInfo{
				Name:           "Go",
				Aliases:        []string{"go", "golang"},
				GrammarContent: goGrammarContent,
				Language:       treesittergo.Language,
			},
			wantFound: true,
		},
		{
			name:  "Get existing language (alternative alias)",
			alias: "js",
			want: &LanguageInfo{
				Name:           "JavaScript",
				Aliases:        []string{"js", "javascript"},
				GrammarContent: jsGrammarContent,
				Language:       treesitterjs.Language,
			},
			wantFound: true,
		},
		{
			name:      "Get non-existing language",
			alias:     "xyz",
			want:      nil,
			wantFound: false,
		},
		{
			name:  "Get language with nil Language function",
			alias: "markdown",
			want: &LanguageInfo{
				Name:           "Markdown",
				Aliases:        []string{"markdown", "md"},
				GrammarContent: mdGrammarContent,
				Language:       nil,
			},
			wantFound: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetLanguageInfo(tt.alias)
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
				t.Errorf("GetLanguageInfo() Name = %v, want %v", got.Name, tt.want.Name)
			}
			if !reflect.DeepEqual(got.Aliases, tt.want.Aliases) {
				t.Errorf("GetLanguageInfo() Aliases = %v, want %v", got.Aliases, tt.want.Aliases)
			}
			if got.GrammarContent != tt.want.GrammarContent {
				t.Errorf("GetLanguageInfo() GrammarContent = %v, want %v", got.GrammarContent, tt.want.GrammarContent)
			}
			// For Language function, just check if both are nil or both are non-nil
			if (got.Language == nil) != (tt.want.Language == nil) {
				t.Errorf("GetLanguageInfo() Language presence = %v, want %v", got.Language == nil, tt.want.Language == nil)
			}
		})
	}
}
func TestGetSupportedLanguages(t *testing.T) {
	want := []string{
		"PHP", "Go", "JavaScript", "TypeScript with JSX", "TypeScript", "Python", "C#", "HTML", "Markdown",
	}
	got := GetSupportedLanguages()

	wantMap := make(map[string]bool, len(want))
	for _, lang := range want {
		wantMap[lang] = true
	}

	gotMap := make(map[string]bool, len(got))
	for _, lang := range got {
		gotMap[lang] = true
	}

	if !reflect.DeepEqual(wantMap, gotMap) {
		t.Errorf("GetSupportedLanguages() = %v, want %v", got, want)
	}
}

// TestLanguageFunctions checks if the Language functions return non-nil values.
func TestLanguageFunctions(t *testing.T) {
	for alias, info := range supportedLanguages {
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

// copyLangInfo creates a copy of a LanguageInfo value.
func copyLangInfo(info LanguageInfo) *LanguageInfo {
	// Create a new LanguageInfo with copies of the values
	newInfo := &LanguageInfo{
		Name:           info.Name,
		Aliases:        make([]string, len(info.Aliases)), // Copy the slice
		GrammarContent: info.GrammarContent,
		Language:       info.Language,
	}
	copy(newInfo.Aliases, info.Aliases) // Copy the slice contents
	return newInfo
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

		err = vm.DoString(nil, `
			local treesitter = require("treesitter")
			local langs = treesitter.supported_languages()
			assert(type(langs) == "table", "supported_languages should return a table")
			
			-- Check that key languages are supported
			assert(langs["Go"] ~= nil, "Go should be supported")
			assert(langs["JavaScript"] ~= nil, "JavaScript should be supported")
			assert(langs["Python"] ~= nil, "Python should be supported")
			assert(langs["PHP"] ~= nil, "PHP should be supported")
			assert(langs["TypeScript"] ~= nil, "TypeScript should be supported")
			assert(langs["HTML"] ~= nil, "HTML should be supported")
			assert(langs["C#"] ~= nil, "C# should be supported")
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

		err = vm.DoString(nil, `
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

	t.Run("grammar content", func(t *testing.T) {
		// Test that each language's grammar content is loaded
		for alias, info := range supportedLanguages {
			assert.NotEmptyf(t, info.GrammarContent, "Grammar content should not be empty for %s", alias)
			assert.NotEmptyf(t, info.Name, "Language name should not be empty for %s", alias)
			assert.NotEmptyf(t, info.Aliases, "Aliases should not be empty for %s", alias)

			// Skip markdown which intentionally has no language binding
			if !strings.EqualFold(info.Name, "Markdown") {
				assert.NotNil(t, info.Language, "Language function should not be nil for %s", alias)
			}
		}
	})

	t.Run("parser error handling", func(t *testing.T) {
		mod := NewTreeSitterModule(logger)
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
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
