package treesitter

import (
	"errors"
	"fmt"

	"github.com/ponyruntime/go-lua"
	treesitter "github.com/tree-sitter/go-tree-sitter"
	"go.uber.org/zap"
)

type entry struct {
	Kind   string            `json:"kind"`
	Match  string            `json:"match"`
	Range  Range             `json:"range"`
	Values map[string]string `json:"values"`
}

type Range struct {
	StartByte  uint  `json:"start_byte"`
	EndByte    uint  `json:"end_byte"`
	StartPoint point `json:"start_point"`
	EndPoint   point `json:"end_point"`
}

type point struct {
	Row    uint `json:"row"`
	Column uint `json:"column"`
}

type Module struct {
	log *zap.Logger
}

func NewTreeSitterModule(log *zap.Logger) *Module {
	return &Module{
		log: log,
	}
}

// Name is the module name.
func (m *Module) Name() string {
	return "treesitter"
}

// Loader is the module loader function.
func (m *Module) Loader(l *lua.LState) int {
	t := l.NewTable()

	lapi := map[string]lua.LGFunction{
		"supportedLanguages": m.supportedLanguages,
		"parse":              m.parse,
		"query":              m.query,
	}

	l.SetFuncs(t, lapi)
	l.Push(t)
	return 1
}

// supportedLanguages returns a table of supported languages.
func (m *Module) supportedLanguages(l *lua.LState) int {
	langs := GetSupportedLanguages()
	table := l.NewTable()
	for _, lang := range langs {
		table.RawSetString(lang, lua.LTrue)
	}
	l.Push(table)
	return 1
}

// parse parses the text into an S-Expression.
func (m *Module) parse(l *lua.LState) int {
	if l.GetTop() != 2 {
		l.ArgError(1, "expected 2 arguments: language, code")
		return 0 // 0 return values when there is an error
	}

	languageAlias := l.CheckString(1)
	code := l.CheckString(2)

	langInfo := GetLanguageInfo(languageAlias)
	if langInfo == nil {
		l.ArgError(1, fmt.Sprintf("unsupported language: %s", languageAlias))
		return 0
	}

	if langInfo.Language == nil {
		l.ArgError(1, fmt.Sprintf("language '%s' does not have a Tree-sitter language binding", languageAlias))
		return 0
	}

	if code == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("code is empty"))
		return 2
	}

	parser := treesitter.NewParser()
	defer parser.Close()

	langFunc := langInfo.Language
	if langFunc == nil {
		l.ArgError(1, fmt.Sprintf("language function for '%s' is not defined", languageAlias))
		return 0
	}

	lang := langFunc()
	err := parser.SetLanguage(treesitter.NewLanguage(lang))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to set language: %s", err)))
		return 2
	}

	tr := parser.Parse([]byte(code), nil)
	defer tr.Close()

	rn := tr.RootNode()
	if rn == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("failed to parse code, root node is nil"))
		return 2
	}

	result := l.NewTable()
	result.RawSetString("sexp", lua.LString(rn.ToSexp()))
	l.Push(result)
	return 1
}

// query queries the code and applies s-expressions on it.
func (m *Module) query(l *lua.LState) int {
	if l.GetTop() != 3 {
		l.ArgError(1, "expected 3 arguments: language, code, query")
		return 0
	}

	languageAlias := l.CheckString(1)
	code := l.CheckString(2)
	queryString := l.CheckString(3)

	langInfo := GetLanguageInfo(languageAlias)
	if langInfo == nil {
		l.ArgError(1, fmt.Sprintf("unsupported language: %s", languageAlias))
		return 0
	}

	if langInfo.Language == nil {
		l.ArgError(1, fmt.Sprintf("language '%s' does not have a Tree-sitter language binding", languageAlias))
		return 0
	}

	if code == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("code is empty"))
		return 2
	}

	parser := treesitter.NewParser()
	defer parser.Close()

	langFunc := langInfo.Language
	if langFunc == nil {
		l.ArgError(1, fmt.Sprintf("language function for '%s' is not defined", languageAlias))
		return 0
	}

	lang := langFunc()
	err := parser.SetLanguage(treesitter.NewLanguage(lang))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to set language: %s", err)))
		return 2
	}

	tr := parser.Parse([]byte(code), nil)
	defer tr.Close()

	rn := tr.RootNode()
	if rn == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("failed to parse code, root node is nil"))
		return 2
	}

	q, qerr := treesitter.NewQuery(treesitter.NewLanguage(lang), queryString)
	if qerr != nil {
		tsqr := &treesitter.QueryError{}
		if errors.As(qerr, &tsqr) {
			m.log.Error("failed to create query", zap.String("query", queryString), zap.String("error", tsqr.Message))
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("failed to create query: %s", tsqr.Message)))
			return 2
		}

		m.log.Error("failed to create query", zap.String("query", queryString), zap.Error(qerr))
		// In case of a generic error -> do not parse. This should not happen, all returned errors are QueryErrors.
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create query: %s", err)))
		return 2
	}
	defer q.Close()

	qc := treesitter.NewQueryCursor()
	defer qc.Close()

	captures := qc.Captures(q, rn, []byte(code))

	ptrToLastEntry := make(map[uint]*entry)
	entries := make([]*entry, 0, 2)

	for mm, idx := captures.Next(); mm != nil; mm, idx = captures.Next() {
		pentry, ok := ptrToLastEntry[mm.PatternIndex]
		if !ok || pentry.Kind == q.CaptureNames()[mm.Captures[idx].Index] {
			tmp := &entry{
				Kind:  q.CaptureNames()[mm.Captures[idx].Index],
				Match: mm.Captures[idx].Node.Utf8Text([]byte(code)),
				Range: Range{
					StartByte: mm.Captures[idx].Node.StartByte(),
					EndByte:   mm.Captures[idx].Node.EndByte(),
					StartPoint: point{
						Row:    mm.Captures[idx].Node.Range().StartPoint.Row,
						Column: mm.Captures[idx].Node.Range().StartPoint.Column,
					},
					EndPoint: point{
						Row:    mm.Captures[idx].Node.Range().EndPoint.Row,
						Column: mm.Captures[idx].Node.Range().EndPoint.Column,
					},
				},
				Values: make(map[string]string),
			}

			entries = append(entries, tmp)
			ptrToLastEntry[mm.PatternIndex] = tmp
			continue
		}

		pentry.Values[q.CaptureNames()[mm.Captures[idx].Index]] = mm.Captures[idx].Node.Utf8Text([]byte(code))
	}

	// root table
	resultsTable := l.NewTable()
	for _, e := range entries {
		entryT := l.NewTable()
		entryT.RawSetString("kind", lua.LString(e.Kind))
		entryT.RawSetString("match", lua.LString(e.Match))
		entryT.RawSetString("range", lua.LString(fmt.Sprintf("%d:%d-%d:%d", e.Range.StartPoint.Row, e.Range.StartPoint.Column, e.Range.EndPoint.Row, e.Range.EndPoint.Column)))
		entryT.RawSetString("start_byte", lua.LNumber(e.Range.StartByte))
		entryT.RawSetString("end_byte", lua.LNumber(e.Range.EndByte))
		values := l.NewTable()
		for k, v := range e.Values {
			values.RawSetString(k, lua.LString(v))
		}
		entryT.RawSetString("values", values)
		resultsTable.Append(entryT)
	}

	// clean up
	for k := range ptrToLastEntry {
		delete(ptrToLastEntry, k)
	}

	result := l.NewTable()
	result.RawSetString("results", resultsTable)
	l.Push(result)
	return 1
}
