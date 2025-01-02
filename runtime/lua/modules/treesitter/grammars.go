package treesitter

import (
	_ "embed"
	"unsafe"

	treesittercsharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	treesittergo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	treesitterhtml "github.com/tree-sitter/tree-sitter-html/bindings/go"
	treesitterjs "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	treesitterphp "github.com/tree-sitter/tree-sitter-php/bindings/go"
	treesitterpython "github.com/tree-sitter/tree-sitter-python/bindings/go"
	treesitterts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

//go:embed grammars/grammar_html.json
var htmlGrammarContent string

//go:embed grammars/grammar_md.json
var mdGrammarContent string

//go:embed grammars/grammar_csharp.json
var csharpGrammarContent string

//go:embed grammars/grammar_js.json
var jsGrammarContent string

//go:embed grammars/grammar_python.json
var pythonGrammarContent string

//go:embed grammars/grammar_ts.json
var tsGrammarContent string

//go:embed grammars/grammar_tsx.json
var tsxGrammarContent string

//go:embed grammars/grammar_go.json
var goGrammarContent string

//go:embed grammars/grammar_php.json
var phpGrammarContent string

// LanguageInfo holds information about a supported language.
type LanguageInfo struct {
	Name           string                // Full language name (e.g., "JavaScript")
	Aliases        []string              // Alternative names or short codes (e.g., ["js", "javascript"])
	GrammarContent string                // The embedded grammar file content
	Language       func() unsafe.Pointer // Function to get the Tree-sitter language object
}

// languageDefinitions contains the core language definitions
var languageDefinitions = []LanguageInfo{
	{
		Name:           "PHP",
		Aliases:        []string{"php"},
		GrammarContent: phpGrammarContent,
		Language:       treesitterphp.LanguagePHP,
	},
	{
		Name:           "Go",
		Aliases:        []string{"go", "golang"},
		GrammarContent: goGrammarContent,
		Language:       treesittergo.Language,
	},
	{
		Name:           "JavaScript",
		Aliases:        []string{"js", "javascript"},
		GrammarContent: jsGrammarContent,
		Language:       treesitterjs.Language,
	},
	{
		Name:           "TypeScript with JSX",
		Aliases:        []string{"tsx"},
		GrammarContent: tsxGrammarContent,
		Language:       treesitterts.LanguageTSX,
	},
	{
		Name:           "TypeScript",
		Aliases:        []string{"ts", "typescript"},
		GrammarContent: tsGrammarContent,
		Language:       treesitterts.LanguageTypescript,
	},
	{
		Name:           "Python",
		Aliases:        []string{"python", "py"},
		GrammarContent: pythonGrammarContent,
		Language:       treesitterpython.Language,
	},
	{
		Name:           "C#",
		Aliases:        []string{"csharp", "c#", "cs"},
		GrammarContent: csharpGrammarContent,
		Language:       treesittercsharp.Language,
	},
	{
		Name:           "HTML",
		Aliases:        []string{"html", "html5"},
		GrammarContent: htmlGrammarContent,
		Language:       treesitterhtml.Language,
	},
	{
		Name:           "Markdown",
		Aliases:        []string{"markdown", "md"},
		GrammarContent: mdGrammarContent,
		Language:       nil,
	},
}

// supportedLanguages is a map of all language aliases to their LanguageInfo
var supportedLanguages map[string]*LanguageInfo

func init() {
	// Initialize the map with enough capacity for all aliases
	totalAliases := 0
	for _, lang := range languageDefinitions {
		totalAliases += len(lang.Aliases)
	}
	supportedLanguages = make(map[string]*LanguageInfo, totalAliases)

	// Map all aliases to their language info
	for i := range languageDefinitions {
		langInfo := &languageDefinitions[i]
		for _, alias := range langInfo.Aliases {
			supportedLanguages[alias] = langInfo
		}
	}
}

// GetLanguageInfo returns the LanguageInfo for a given language alias.
func GetLanguageInfo(alias string) *LanguageInfo {
	return supportedLanguages[alias]
}

// GetSupportedLanguages returns a list of supported language names.
func GetSupportedLanguages() []string {
	seen := make(map[string]bool)
	names := make([]string, 0, len(languageDefinitions))
	for _, langInfo := range languageDefinitions {
		if !seen[langInfo.Name] {
			names = append(names, langInfo.Name)
			seen[langInfo.Name] = true
		}
	}
	return names
}
