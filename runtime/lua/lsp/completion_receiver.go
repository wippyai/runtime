package lsp

import (
	"github.com/wippyai/go-lua/compiler/ast"
	"github.com/wippyai/go-lua/compiler/cfg"
	"github.com/wippyai/go-lua/compiler/check"
	"github.com/wippyai/go-lua/compiler/check/api"
	"github.com/wippyai/go-lua/compiler/check/hooks"
	"github.com/wippyai/go-lua/compiler/check/scope"
	"github.com/wippyai/go-lua/compiler/parse"
	"github.com/wippyai/go-lua/compiler/stdlib"
	"github.com/wippyai/go-lua/types/db"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/query/core"
	"github.com/wippyai/go-lua/types/typ"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/lsp/indexing"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
)

const completionPlaceholder = "__wippy_lsp_completion__"

type exprChecker struct {
	db          *db.DB
	checker     *check.Checker
	builtinHash string
	depsHash    string
}

func (s *Service) ResolveReceiverTypeAt(fileID string, line, col int) typ.Type {
	if fileID == "" || line < 1 || col < 1 {
		return nil
	}

	source, _, ok := s.documentSnapshot(fileID)
	if !ok || source == "" {
		return nil
	}

	member := memberLocationAt(source, line, col)
	stmts, placeholder, err := parseCompletionSource(source, fileID, line, col, member)
	if err != nil || len(stmts) == 0 {
		return nil
	}

	s.mu.RLock()
	provider := s.provider
	s.mu.RUnlock()
	if provider == nil {
		return nil
	}

	parsedID := registry.ParseID(fileID)
	var deps map[string]*io.Manifest
	if parsedID != (registry.ID{}) {
		deps = provider.DependencyManifests(parsedID)
	}

	var sess *check.Session
	s.completionMu.Lock()
	checker := s.ensureCompletionCheckerLocked(provider, deps)
	if checker != nil {
		sess = checker.checker.CheckChunk(stmts, fileID)
		checker.checker.ClearCache()
	}
	s.completionMu.Unlock()

	if sess == nil {
		return nil
	}
	defer sess.Release()

	fn, result := funcResultAtPosition(sess, line, col)
	if result == nil {
		if col > 1 {
			fn, result = funcResultAtPosition(sess, line, col-1)
		}
	}
	if result == nil || result.Graph == nil {
		return nil
	}

	receiver := findMemberReceiver(fn, line, col, placeholder, member)
	if receiver == nil && col > 1 {
		receiver = findMemberReceiver(fn, line, col-1, placeholder, member)
	}
	if receiver == nil {
		receiver = findExprAt(fn, line, col)
	}
	if receiver == nil && col > 1 {
		receiver = findExprAt(fn, line, col-1)
	}
	if receiver == nil {
		return nil
	}

	point, ok := pointForPosition(result.Graph, line, col)
	if !ok && col > 1 {
		point, ok = pointForPosition(result.Graph, line, col-1)
	}
	if !ok {
		point = result.Graph.Entry()
	}

	if result.NarrowSynth == nil {
		if ident, ok := receiver.(*ast.IdentExpr); ok {
			return s.resolveIdentifierType(fileID, ident.Value)
		}
		return nil
	}

	return result.NarrowSynth.TypeOf(receiver, point)
}

func (s *Service) documentSnapshot(id string) (string, int, bool) {
	if id == "" {
		return "", 0, false
	}
	parsed := registry.ParseID(id)
	if parsed == (registry.ID{}) {
		return "", 0, false
	}

	s.mu.RLock()
	documents := s.documents
	provider := s.provider
	s.mu.RUnlock()

	if documents != nil {
		if doc, ok := documents.Get(parsed); ok {
			return doc.Text, doc.Version, true
		}
	}

	if provider != nil {
		node, err := provider.Node(parsed)
		if err == nil {
			return node.Source, 0, node.Source != ""
		}
	}

	return "", 0, false
}

func (s *Service) ensureCompletionCheckerLocked(provider indexing.Provider, deps map[string]*io.Manifest) *exprChecker {
	if provider == nil {
		s.completionChecker = nil
		return nil
	}

	builtinHash := provider.BuiltinManifestHash()
	depsHash := hashManifests(deps)
	if s.completionChecker != nil && s.completionChecker.builtinHash == builtinHash && s.completionChecker.depsHash == depsHash {
		return s.completionChecker
	}

	s.completionChecker = newExprChecker(provider, deps, depsHash)
	return s.completionChecker
}

func newExprChecker(provider indexing.Provider, deps map[string]*io.Manifest, depsHash string) *exprChecker {
	if provider == nil {
		return nil
	}

	builtins := make(map[string]typ.Type)
	manifests := make(map[string]*io.Manifest)

	for _, mod := range provider.ModuleDefs() {
		if mod == nil || mod.Types == nil || mod.Name == "" {
			continue
		}
		manifest := mod.Types()
		if manifest == nil {
			continue
		}
		manifests[mod.Name] = manifest
		if manifest.Export != nil {
			builtins[mod.Name] = manifest.Export
		}
		for name, t := range manifest.AllGlobals() {
			builtins[name] = t
		}
	}

	base := scope.NewWithBuiltins()
	globalTypes := make(map[string]typ.Type)
	for name, t := range stdlib.Library() {
		globalTypes[name] = t
	}
	for name, t := range builtins {
		globalTypes[name] = t
	}

	database := db.New()
	for path, manifest := range manifests {
		database.Connect(path, manifest)
	}
	for alias, manifest := range deps {
		if manifest != nil {
			database.Connect(alias, manifest)
		}
	}

	types := core.NewEngineWithStdlib(stdlib.EngineConfig())
	opts := []check.Option{
		hooks.WithAssign(),
		hooks.WithReturn(),
		hooks.WithCall(),
		hooks.WithField(),
	}

	checker := check.NewChecker(database, check.Deps{
		Types:       types,
		Stdlib:      base,
		GlobalTypes: globalTypes,
		Resolver: &core.FuncResolver{
			FieldFunc: core.Field,
			IndexFunc: core.Index,
		},
	}, opts...)

	return &exprChecker{
		db:          database,
		checker:     checker,
		builtinHash: provider.BuiltinManifestHash(),
		depsHash:    depsHash,
	}
}

func hashManifests(deps map[string]*io.Manifest) string {
	if len(deps) == 0 {
		return ""
	}
	keys := make([]string, 0, len(deps))
	for key := range deps {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	h := fnv.New64a()
	for _, key := range keys {
		_, _ = h.Write([]byte(key))
		if manifest := deps[key]; manifest != nil {
			_, _ = h.Write([]byte(":"))
			_, _ = h.Write([]byte(strconv.FormatUint(manifest.Version, 10)))
		} else {
			_, _ = h.Write([]byte(":nil"))
		}
		_, _ = h.Write([]byte(";"))
	}
	return strconv.FormatUint(h.Sum64(), 16)
}

type memberLocation struct {
	Line        int
	SepIndex    int
	PrefixStart int
	Sep         byte
	Valid       bool
}

func parseCompletionSource(source string, fileID string, line, col int, member memberLocation) ([]ast.Stmt, string, error) {
	stmts, err := parse.ParseString(source, fileID)
	if err == nil {
		return stmts, "", nil
	}

	patched, placeholder := patchSourceForCompletion(source, line, col, member)
	if patched != source {
		if stmts, err2 := parse.ParseString(patched, fileID); err2 == nil {
			return stmts, placeholder, nil
		} else {
			err = err2
		}
	}

	stub := completionStub(patched, line)
	if stub != "" {
		if stmts, err2 := parse.ParseString(stub, fileID); err2 == nil {
			return stmts, placeholder, nil
		} else {
			err = err2
		}
	}

	return nil, "", err
}

func patchSourceForCompletion(source string, line, col int, member memberLocation) (string, string) {
	if !member.Valid || line < 1 || col < 1 {
		return source, ""
	}

	lineText, lineStart := lineAtWithOffset(source, line)
	if lineText == "" {
		return source, ""
	}

	col0 := col - 1
	if col0 < 0 {
		col0 = 0
	}
	if col0 > len(lineText) {
		col0 = len(lineText)
	}

	sepIdx := member.SepIndex
	prefixStart := member.PrefixStart
	sep := member.Sep
	if sepIdx < 0 || sepIdx >= len(lineText) {
		return source, ""
	}

	insertPlaceholder := col0 == prefixStart
	if !insertPlaceholder && col0 > prefixStart {
		onlySpace := true
		for i := prefixStart; i < col0 && i < len(lineText); i++ {
			if !isSpaceChar(lineText[i]) {
				onlySpace = false
				break
			}
		}
		if onlySpace {
			insertPlaceholder = true
		}
	}
	placeholder := ""
	if insertPlaceholder {
		placeholder = completionPlaceholder
	}

	if sep == '.' && !insertPlaceholder {
		return source, ""
	}

	globalSep := lineStart + sepIdx
	globalInsert := lineStart + prefixStart
	if globalSep < 0 || globalSep >= len(source) || globalInsert < 0 || globalInsert > len(source) {
		return source, ""
	}

	var b strings.Builder
	b.Grow(len(source) + len(placeholder))
	b.WriteString(source[:globalSep])
	if sep == ':' {
		b.WriteByte('.')
	} else {
		b.WriteByte(sep)
	}
	b.WriteString(source[globalSep+1 : globalInsert])
	if insertPlaceholder {
		b.WriteString(placeholder)
	}
	b.WriteString(source[globalInsert:])
	return b.String(), placeholder
}

func completionStub(source string, line int) string {
	if line < 1 {
		return ""
	}
	source = maybeWrapCompletionLine(source, line)
	end := lineEndOffset(source, line)
	if end == 0 {
		return ""
	}
	prefix := source[:end]
	closers := balanceBlocks(prefix)
	if len(closers) == 0 {
		return prefix
	}
	var b strings.Builder
	b.WriteString(prefix)
	if len(prefix) > 0 && prefix[len(prefix)-1] != '\n' {
		b.WriteByte('\n')
	}
	for i, close := range closers {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(close)
	}
	b.WriteByte('\n')
	return b.String()
}

func maybeWrapCompletionLine(source string, line int) string {
	lineText, start := lineAtWithOffset(source, line)
	if lineText == "" {
		return source
	}
	if !strings.Contains(lineText, completionPlaceholder) {
		return source
	}

	trimmed := strings.TrimSpace(lineText)
	if trimmed == "" {
		return source
	}

	lower := strings.ToLower(strings.TrimLeft(trimmed, " \t"))
	switch {
	case strings.HasPrefix(lower, "local "):
		return source
	case strings.HasPrefix(lower, "return "):
		return source
	case strings.HasPrefix(lower, "if "):
		return source
	case strings.HasPrefix(lower, "for "):
		return source
	case strings.HasPrefix(lower, "while "):
		return source
	case strings.HasPrefix(lower, "repeat"):
		return source
	case strings.HasPrefix(lower, "function "):
		return source
	case strings.HasPrefix(lower, "do"):
		return source
	case strings.HasPrefix(lower, "end"):
		return source
	}

	if strings.Contains(trimmed, "=") {
		return source
	}
	if looksLikeCallStatement(trimmed) {
		return source
	}

	indent := lineText[:len(lineText)-len(strings.TrimLeft(lineText, " \t"))]
	replacement := indent + "local " + completionPlaceholder + "_stmt = " + strings.TrimSpace(lineText)
	return source[:start] + replacement + source[start+len(lineText):]
}

func looksLikeCallStatement(trimmed string) bool {
	if trimmed == "" {
		return false
	}
	last := trimmed[len(trimmed)-1]
	switch last {
	case ')', ']', '}', '"', '\'':
		return true
	}
	return false
}

func lineAtWithOffset(text string, line int) (string, int) {
	if line < 1 {
		return "", 0
	}
	start := 0
	current := 1
	for i := 0; i <= len(text); i++ {
		if i == len(text) || text[i] == '\n' {
			if current == line {
				return strings.TrimRight(text[start:i], "\r"), start
			}
			current++
			start = i + 1
		}
	}
	return "", 0
}

func lineEndOffset(text string, line int) int {
	lineText, start := lineAtWithOffset(text, line)
	if lineText == "" {
		return 0
	}
	end := start + len(lineText)
	if end < len(text) {
		if text[end] == '\r' {
			end++
			if end < len(text) && text[end] == '\n' {
				end++
			}
		} else if text[end] == '\n' {
			end++
			if end < len(text) && text[end] == '\r' {
				end++
			}
		}
	}
	return end
}

func memberLocationAt(source string, line, col int) memberLocation {
	loc := memberLocation{}
	if line < 1 || col < 1 {
		return loc
	}
	lineText, _ := lineAtWithOffset(source, line)
	if lineText == "" {
		return loc
	}
	col0 := col - 1
	if col0 < 0 {
		col0 = 0
	}
	if col0 > len(lineText) {
		col0 = len(lineText)
	}
	sepIdx, sep, prefixStart := memberSeparatorIndex(lineText, col0)
	if sepIdx < 0 {
		return loc
	}
	loc.Line = line
	loc.SepIndex = sepIdx
	loc.PrefixStart = prefixStart
	loc.Sep = sep
	loc.Valid = true
	return loc
}

func memberSeparatorIndex(line string, col int) (int, byte, int) {
	if col < 0 {
		return -1, 0, 0
	}
	if col > len(line) {
		col = len(line)
	}
	// Skip whitespace to tolerate cursor positions after a trailing space.
	end := col
	for end > 0 && isSpaceChar(line[end-1]) {
		end--
	}
	if end == 0 {
		return -1, 0, 0
	}

	start := end
	for start > 0 && isIdentChar(line[start-1]) {
		start--
	}

	// If no prefix was typed, the separator should be immediately before the cursor.
	if start == end {
		sepIdx := end - 1
		if sepIdx < 0 {
			return -1, 0, start
		}
		sep := line[sepIdx]
		if sep != '.' && sep != ':' {
			return -1, 0, start
		}
		return sepIdx, sep, start
	}

	if start == 0 {
		return -1, 0, start
	}
	sep := line[start-1]
	if sep != '.' && sep != ':' {
		return -1, 0, start
	}
	return start - 1, sep, start
}

func isSpaceChar(b byte) bool {
	return b == ' ' || b == '\t'
}

type blockClose uint8

const (
	closeEnd blockClose = iota
	closeUntil
)

func balanceBlocks(src string) []string {
	if src == "" {
		return nil
	}
	stack := make([]blockClose, 0, 8)
	pendingDo := 0
	for i := 0; i < len(src); {
		ch := src[i]
		if ch == '-' && i+1 < len(src) && src[i+1] == '-' {
			if level, ok := longBracketLevel(src, i+2); ok {
				i = skipLongBracket(src, i+2, level)
				continue
			}
			i = skipLineComment(src, i+2)
			continue
		}
		if ch == '\'' || ch == '"' {
			i = skipQuotedString(src, i, ch)
			continue
		}
		if ch == '[' {
			if level, ok := longBracketLevel(src, i); ok {
				i = skipLongBracket(src, i, level)
				continue
			}
		}
		if isIdentStart(ch) {
			start := i
			i++
			for i < len(src) && isIdentChar(src[i]) {
				i++
			}
			ident := src[start:i]
			switch ident {
			case "function", "if", "for", "while", "interface":
				stack = append(stack, closeEnd)
				if ident == "for" || ident == "while" {
					pendingDo++
				}
			case "do":
				if pendingDo > 0 {
					pendingDo--
				} else {
					stack = append(stack, closeEnd)
				}
			case "repeat":
				stack = append(stack, closeUntil)
			case "end":
				stack = popBlock(stack, closeEnd)
			case "until":
				stack = popBlock(stack, closeUntil)
			}
			continue
		}
		i++
	}
	if len(stack) == 0 {
		return nil
	}
	closers := make([]string, 0, len(stack))
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == closeUntil {
			closers = append(closers, "until true")
		} else {
			closers = append(closers, "end")
		}
	}
	return closers
}

func popBlock(stack []blockClose, want blockClose) []blockClose {
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == want {
			return append(stack[:i], stack[i+1:]...)
		}
	}
	return stack
}

func skipLineComment(src string, i int) int {
	for i < len(src) {
		if src[i] == '\n' {
			return i
		}
		i++
	}
	return i
}

func skipQuotedString(src string, i int, quote byte) int {
	i++
	for i < len(src) {
		ch := src[i]
		if ch == '\\' {
			i += 2
			continue
		}
		if ch == quote {
			return i + 1
		}
		if ch == '\n' {
			return i + 1
		}
		i++
	}
	return i
}

func longBracketLevel(src string, i int) (int, bool) {
	if i >= len(src) || src[i] != '[' {
		return 0, false
	}
	j := i + 1
	for j < len(src) && src[j] == '=' {
		j++
	}
	if j < len(src) && src[j] == '[' {
		return j - (i + 1), true
	}
	return 0, false
}

func skipLongBracket(src string, i int, level int) int {
	i++
	for i < len(src) {
		if src[i] == ']' {
			j := i + 1
			for j < len(src) && src[j] == '=' {
				j++
			}
			if j < len(src) && src[j] == ']' && j-(i+1) == level {
				return j + 1
			}
		}
		i++
	}
	return i
}

func funcResultAtPosition(sess *check.Session, line, col int) (*ast.FunctionExpr, *api.FuncResult) {
	if sess == nil {
		return nil, nil
	}
	results := sess.ResultsMap()
	var bestFn *ast.FunctionExpr
	var bestSpan diag.Span
	var bestResult *api.FuncResult

	for fn, result := range results {
		if fn == nil || result == nil {
			continue
		}
		span := ast.SpanOf(fn)
		if !spanContains(span, line, col) {
			continue
		}
		if bestFn == nil || spanSmaller(span, bestSpan) {
			bestFn = fn
			bestSpan = span
			bestResult = result
		}
	}

	if bestResult != nil {
		return bestFn, bestResult
	}

	return sess.RootFuncNode(), sess.RootResultValue()
}

func findMemberReceiver(fn *ast.FunctionExpr, line, col int, placeholder string, member memberLocation) ast.Expr {
	if fn == nil {
		return nil
	}
	var bestObj ast.Expr
	var bestSpan diag.Span
	memberKeyCol := 0
	if member.Valid {
		memberKeyCol = member.PrefixStart + 1
	}

	visit := func(expr ast.Expr) {
		ag, ok := expr.(*ast.AttrGetExpr)
		if !ok {
			return
		}
		keySpan := ast.SpanOf(ag.Key)
		attrSpan := ast.SpanOf(ag)
		match := false

		if placeholder != "" {
			switch key := ag.Key.(type) {
			case *ast.IdentExpr:
				if key.Value == placeholder {
					match = true
				}
			case *ast.StringExpr:
				if key.Value == placeholder {
					match = true
				}
			}
		} else if member.Valid && keySpan.Valid() && keySpan.StartLine == member.Line && keySpan.StartCol == memberKeyCol {
			if spanContains(attrSpan, line, col) || spanContains(keySpan, line, col) {
				match = true
			}
		} else {
			if spanContains(keySpan, line, col) {
				match = true
			} else if !keySpan.Valid() && spanContains(attrSpan, line, col) {
				match = true
			}
		}

		if match {
			if bestObj == nil || spanSmaller(attrSpan, bestSpan) {
				bestObj = ag.Object
				bestSpan = attrSpan
			}
		}
	}

	walkStmts(fn.Stmts, visit)
	return bestObj
}

func findExprAt(fn *ast.FunctionExpr, line, col int) ast.Expr {
	if fn == nil {
		return nil
	}
	var best ast.Expr
	var bestSpan diag.Span

	visit := func(expr ast.Expr) {
		span := ast.SpanOf(expr)
		if !spanContains(span, line, col) {
			return
		}
		if best == nil || spanSmaller(span, bestSpan) {
			best = expr
			bestSpan = span
		}
	}

	walkStmts(fn.Stmts, visit)
	return best
}

func walkStmts(stmts []ast.Stmt, visit func(ast.Expr)) {
	for _, stmt := range stmts {
		walkStmt(stmt, visit)
	}
}

func walkStmt(stmt ast.Stmt, visit func(ast.Expr)) {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		for _, expr := range s.Lhs {
			walkExpr(expr, visit)
		}
		for _, expr := range s.Rhs {
			walkExpr(expr, visit)
		}
	case *ast.LocalAssignStmt:
		for _, expr := range s.Exprs {
			walkExpr(expr, visit)
		}
	case *ast.FuncCallStmt:
		walkExpr(s.Expr, visit)
	case *ast.DoBlockStmt:
		walkStmts(s.Stmts, visit)
	case *ast.WhileStmt:
		walkExpr(s.Condition, visit)
		walkStmts(s.Stmts, visit)
	case *ast.RepeatStmt:
		walkStmts(s.Stmts, visit)
		walkExpr(s.Condition, visit)
	case *ast.IfStmt:
		walkExpr(s.Condition, visit)
		walkStmts(s.Then, visit)
		walkStmts(s.Else, visit)
	case *ast.NumberForStmt:
		walkExpr(s.Init, visit)
		walkExpr(s.Limit, visit)
		walkExpr(s.Step, visit)
		walkStmts(s.Stmts, visit)
	case *ast.GenericForStmt:
		for _, expr := range s.Exprs {
			walkExpr(expr, visit)
		}
		walkStmts(s.Stmts, visit)
	case *ast.FuncDefStmt:
		if s.Name != nil {
			walkExpr(s.Name.Func, visit)
			walkExpr(s.Name.Receiver, visit)
		}
		if s.Func != nil {
			walkExpr(s.Func, visit)
		}
	case *ast.ReturnStmt:
		for _, expr := range s.Exprs {
			walkExpr(expr, visit)
		}
	}
}

func walkExpr(expr ast.Expr, visit func(ast.Expr)) {
	if expr == nil {
		return
	}

	visit(expr)

	switch e := expr.(type) {
	case *ast.AttrGetExpr:
		walkExpr(e.Object, visit)
		walkExpr(e.Key, visit)
	case *ast.TableExpr:
		for _, field := range e.Fields {
			if field == nil {
				continue
			}
			walkExpr(field.Key, visit)
			walkExpr(field.Value, visit)
		}
	case *ast.FuncCallExpr:
		walkExpr(e.Func, visit)
		walkExpr(e.Receiver, visit)
		for _, arg := range e.Args {
			walkExpr(arg, visit)
		}
	case *ast.LogicalOpExpr:
		walkExpr(e.Lhs, visit)
		walkExpr(e.Rhs, visit)
	case *ast.RelationalOpExpr:
		walkExpr(e.Lhs, visit)
		walkExpr(e.Rhs, visit)
	case *ast.StringConcatOpExpr:
		walkExpr(e.Lhs, visit)
		walkExpr(e.Rhs, visit)
	case *ast.ArithmeticOpExpr:
		walkExpr(e.Lhs, visit)
		walkExpr(e.Rhs, visit)
	case *ast.UnaryMinusOpExpr:
		walkExpr(e.Expr, visit)
	case *ast.UnaryNotOpExpr:
		walkExpr(e.Expr, visit)
	case *ast.UnaryLenOpExpr:
		walkExpr(e.Expr, visit)
	case *ast.UnaryBNotOpExpr:
		walkExpr(e.Expr, visit)
	case *ast.FunctionExpr:
		// Do not descend into nested function bodies.
	case *ast.CastExpr:
		walkExpr(e.Expr, visit)
	case *ast.NonNilAssertExpr:
		walkExpr(e.Expr, visit)
	}
}

func pointForPosition(graph *cfg.Graph, line, col int) (cfg.Point, bool) {
	if graph == nil {
		return 0, false
	}

	var bestPoint cfg.Point
	var bestSpan diag.Span
	found := false

	graph.EachNode(func(p cfg.Point, info cfg.NodeInfo) {
		for _, span := range nodeSpans(info) {
			if !spanContains(span, line, col) {
				continue
			}
			if !found || spanSmaller(span, bestSpan) {
				bestPoint = p
				bestSpan = span
				found = true
			}
		}
	})

	if !found {
		graph.EachNode(func(p cfg.Point, info cfg.NodeInfo) {
			for _, span := range nodeSpans(info) {
				if !spanContainsLine(span, line) {
					continue
				}
				if !found || spanSmaller(span, bestSpan) {
					bestPoint = p
					bestSpan = span
					found = true
				}
			}
		})
	}

	return bestPoint, found
}

func nodeSpans(info cfg.NodeInfo) []diag.Span {
	if info == nil {
		return nil
	}

	spans := make([]diag.Span, 0, 4)
	add := func(node ast.PositionHolder) {
		if node == nil {
			return
		}
		span := ast.SpanOf(node)
		if span.Valid() {
			spans = append(spans, span)
		}
	}

	switch n := info.(type) {
	case *cfg.AssignInfo:
		if n.Stmt != nil {
			add(n.Stmt)
		}
		for _, expr := range n.Sources {
			add(expr)
		}
		for _, target := range n.Targets {
			if target.Expr != nil {
				add(target.Expr)
			}
		}
	case *cfg.CallInfo:
		add(n.Callee)
		add(n.Receiver)
		for _, arg := range n.Args {
			add(arg)
		}
	case *cfg.ReturnInfo:
		if n.Stmt != nil {
			add(n.Stmt)
		}
		for _, expr := range n.Exprs {
			add(expr)
		}
	case *cfg.BranchInfo:
		add(n.Condition)
	case *cfg.FuncDefInfo:
		add(n.FuncExpr)
		add(n.Receiver)
	case *cfg.TypeDefInfo:
		if n.TypeExpr != nil {
			add(n.TypeExpr)
		}
	}

	return spans
}

func spanContains(span diag.Span, line, col int) bool {
	if !span.Valid() {
		return false
	}
	endLine := span.EndLine
	endCol := span.EndCol
	if endLine == 0 {
		endLine = span.StartLine
	}
	if endCol == 0 {
		endCol = span.StartCol
	}
	if line < span.StartLine || line > endLine {
		return false
	}
	if line == span.StartLine && col < span.StartCol {
		return false
	}
	if line == endLine && col > endCol {
		return false
	}
	return true
}

func spanContainsLine(span diag.Span, line int) bool {
	if !span.Valid() {
		return false
	}
	endLine := span.EndLine
	if endLine == 0 {
		endLine = span.StartLine
	}
	return line >= span.StartLine && line <= endLine
}

func spanSmaller(a, b diag.Span) bool {
	if !a.Valid() {
		return false
	}
	if !b.Valid() {
		return true
	}
	return spanSize(a) < spanSize(b)
}

func spanSize(span diag.Span) int {
	if !span.Valid() {
		return 0
	}
	endLine := span.EndLine
	endCol := span.EndCol
	if endLine == 0 {
		endLine = span.StartLine
	}
	if endCol == 0 {
		endCol = span.StartCol
	}
	lineSpan := endLine - span.StartLine
	colSpan := endCol - span.StartCol
	if lineSpan < 0 {
		lineSpan = 0
	}
	if colSpan < 0 {
		colSpan = 0
	}
	return lineSpan*1000000 + colSpan
}
