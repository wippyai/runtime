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
	Aliases        []string              // Alternative names or short codes (e.g., "js")
	GrammarContent string                // The embedded grammar file content
	Language       func() unsafe.Pointer // Function to get the Tree-sitter language object
}

// supportedLanguages is a map of language aliases to LanguageInfo.
var supportedLanguages = map[string]LanguageInfo{
	"php": {
		Name:           "PHP",
		Aliases:        []string{"php"},
		GrammarContent: phpGrammarContent,
		Language:       treesitterphp.LanguagePHP,
	},
	"go": {
		Name:           "Go",
		Aliases:        []string{"go"},
		GrammarContent: goGrammarContent,
		Language:       treesittergo.Language,
	},
	"js": {
		Name:           "JavaScript",
		Aliases:        []string{"js", "javascript"},
		GrammarContent: jsGrammarContent,
		Language:       treesitterjs.Language,
	},
	"tsx": {
		Name:           "TypeScript with JSX",
		Aliases:        []string{"tsx"},
		GrammarContent: tsxGrammarContent,
		Language:       treesitterts.LanguageTSX,
	},
	"ts": {
		Name:           "TypeScript",
		Aliases:        []string{"ts", "typescript"},
		GrammarContent: tsGrammarContent,
		Language:       treesitterts.LanguageTypescript,
	},
	"python": {
		Name:           "Python",
		Aliases:        []string{"python", "py"}, // Added "py" as a common alias
		GrammarContent: pythonGrammarContent,
		Language:       treesitterpython.Language,
	},
	"csharp": {
		Name:           "C#",
		Aliases:        []string{"csharp", "c#"},
		GrammarContent: csharpGrammarContent,
		Language:       treesittercsharp.Language,
	},
	"html": {
		Name:           "HTML",
		Aliases:        []string{"html", "html5"},
		GrammarContent: htmlGrammarContent,
		Language:       treesitterhtml.Language,
	},
	"markdown": {
		Name:           "Markdown",
		Aliases:        []string{"markdown", "md"}, // Added "md" as a common alias
		GrammarContent: mdGrammarContent,
		Language:       nil, // Markdown doesn't seem to have a direct Tree-sitter language binding, handle accordingly.
	},
}

// GetLanguageInfo returns the LanguageInfo for a given language alias.
func GetLanguageInfo(alias string) *LanguageInfo {
	if info, ok := supportedLanguages[alias]; ok {
		return &info
	}
	return nil
}

// GetSupportedLanguages returns a list of supported language names.
func GetSupportedLanguages() []string {
	names := make([]string, 0, len(supportedLanguages))
	for _, info := range supportedLanguages {
		names = append(names, info.Name)
	}
	return names
}
