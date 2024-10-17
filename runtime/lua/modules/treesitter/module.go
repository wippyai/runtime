package treesitter

import (
	"errors"
	"fmt"
	"unsafe"

	"git.spiralscout.com/estimation-engine/go-lua"
	treesitter "github.com/tree-sitter/go-tree-sitter"
	treesittercsharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	treesittergo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	treesitterhtml "github.com/tree-sitter/tree-sitter-html/bindings/go"
	treesitterjs "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	treesitterphp "github.com/tree-sitter/tree-sitter-php/bindings/go"
	treesitterpython "github.com/tree-sitter/tree-sitter-python/bindings/go"
	treesitterts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
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

func NewModule(log *zap.Logger) *Module {
	return &Module{
		log: log,
	}
}

func (m *Module) grammar(l *lua.LState) int {
	table := l.NewTable()
	table.RawSetString("markdown", lua.LString(mdgrammar))
	table.RawSetString("html", lua.LString(htmlgrammar))
	table.RawSetString("csharp", lua.LString(csharpgrammar))
	table.RawSetString("go", lua.LString(gogrammar))
	table.RawSetString("js", lua.LString(jsgrammar))
	table.RawSetString("php", lua.LString(phpgrammar))
	table.RawSetString("python", lua.LString(pythongrammar))
	table.RawSetString("ts", lua.LString(tsgrammar))
	table.RawSetString("tsx", lua.LString(tsxgrammar))
	l.Push(table)

	return 1
}

// parse parses the text into an S-Expression
// In: string language + code (string)
// Out: string (parsed data) or error
func (m *Module) toSExpr(l *lua.LState) int {
	if l.GetTop() != 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("invalid number of arguments, expected 2, provided %d", l.GetTop())))
		return 2
	}

	language := l.CheckString(1)
	if language == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("language must be a non-empty string"))
		return 2
	}

	code := l.CheckString(2)
	if code == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("code must be a non-empty string"))
		return 2
	}

	lang := parseLang(language)
	if lang == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("language %s is not supported, supported languages: go, php", language)))
		return 2
	}

	parser := treesitter.NewParser()
	defer parser.Close()

	err := parser.SetLanguage(treesitter.NewLanguage(lang))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to set language: %s", err)))
	}

	tr := parser.Parse([]byte(code), nil)
	defer tr.Close()

	rn := tr.RootNode()
	if rn == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("failed to parse code, root node is nil"))
		return 2
	}

	// push the result
	l.Push(lua.LString(rn.ToSexp()))
	l.Push(lua.LNil)

	return 2
}

// query queries the code and applies s-expressions on it
// In: string language + code (string) + query (string)
// Out: string (parsed data separated by \n (new-line)) or error
func (m *Module) query(l *lua.LState) int {
	if l.GetTop() != 3 {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("invalid number of arguments, expected 2, provided %d", l.GetTop())))
		return 2
	}

	language := l.CheckString(1)
	if language == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("language must be a non-empty string"))
		return 2
	}

	code := l.CheckString(2)
	if code == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("code must be a non-empty string"))
		return 2
	}

	query := l.CheckString(3)
	if query == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("query must be a non-empty string"))
		return 2
	}

	lang := parseLang(language)
	if lang == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("language %s is not supported, supported languages: go, php", language)))
		return 2
	}

	parser := treesitter.NewParser()
	defer parser.Close()

	err := parser.SetLanguage(treesitter.NewLanguage(lang))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to set language: %s", err)))
	}

	tr := parser.Parse([]byte(code), nil)
	defer tr.Close()

	rn := tr.RootNode()
	if rn == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("failed to parse code, root node is nil"))
		return 2
	}

	q, qerr := treesitter.NewQuery(treesitter.NewLanguage(lang), query)
	if qerr != nil {
		tsqr := &treesitter.QueryError{}
		if errors.As(qerr, &tsqr) {
			m.log.Error("failed to create query", zap.String("query", query), zap.String("error", tsqr.Message))
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("failed to create query: %s", tsqr.Message)))
			return 2
		}

		m.log.Error("failed to create query", zap.String("query", query), zap.Error(qerr))
		// In case of a generic error -> do not parse. This should not happen, all returned errors are QueryErrors.
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create query: %s", err)))
		return 2
	}

	qc := treesitter.NewQueryCursor()
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

			// if the CaptureName is not in a map or this kind is already in the map, we're storing the entry and storing the pointer to it in the ptrToLastEntry map
			entries = append(entries, tmp)
			// here we are storing the pointer to the last entry of the pattern
			ptrToLastEntry[mm.PatternIndex] = tmp
			continue
		}

		// by this pointer, we're accessing the entries slice and updating the values
		pentry.Values[q.CaptureNames()[mm.Captures[idx].Index]] = mm.Captures[idx].Node.Utf8Text([]byte(code))
	}

	// root table
	table := l.NewTable()
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
		// append e table to root table
		table.Append(entryT)
	}

	// clean up
	for k := range ptrToLastEntry {
		delete(ptrToLastEntry, k)
	}

	l.Push(table)
	l.Push(lua.LNil)

	return 2
}

func parseLang(lang string) unsafe.Pointer {
	switch lang {
	case "php":
		return treesitterphp.LanguagePHP()
	case "go":
		return treesittergo.Language()
	case "js":
		return treesitterjs.Language()
	case "tsx":
		return treesitterts.LanguageTSX()
	case "ts":
		return treesitterts.LanguageTypescript()
	case "python":
		return treesitterpython.Language()
	case "csharp", "c#":
		return treesittercsharp.Language()
	case "html", "html5":
		return treesitterhtml.Language()
	default:
		return nil
	}
}
