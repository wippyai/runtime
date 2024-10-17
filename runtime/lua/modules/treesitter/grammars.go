package treesitter

import (
	_ "embed"
)

//go:embed grammars/grammar_html.json
var mdgrammar string

//go:embed grammars/grammar_md.json
var htmlgrammar string

//go:embed grammars/grammar_csharp.json
var csharpgrammar string

//go:embed grammars/grammar_js.json
var jsgrammar string

//go:embed grammars/grammar_python.json
var pythongrammar string

//go:embed grammars/grammar_ts.json
var tsgrammar string

//go:embed grammars/grammar_tsx.json
var tsxgrammar string

//go:embed grammars/grammar_go.json
var gogrammar string

//go:embed grammars/grammar_php.json
var phpgrammar string
