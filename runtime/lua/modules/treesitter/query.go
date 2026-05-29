// SPDX-License-Identifier: MPL-2.0

//go:build treesitter

package treesitter

import (
	"context"
	"fmt"
	"regexp"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const typeQuery = "treesitter.Query"

// pushQuery pushes a Query userdata to the stack
func pushQuery(l *lua.LState, q *QueryWrapper) {
	value.PushTypedUserData(l, q, typeQuery)
}

// QueryWrapper wraps a tree-sitter Query and QueryCursor for Lua integration
type QueryWrapper struct {
	query         *treesitter.Query
	cursor        *treesitter.QueryCursor
	cancelCleanup func()
	source        string
	closed        bool
}

// NewQuery creates a new query wrapper with proper resource store integration
func NewQuery(ctx context.Context, query *treesitter.Query, cursor *treesitter.QueryCursor) *QueryWrapper {
	wrapper := &QueryWrapper{
		query:  query,
		cursor: cursor,
	}

	// Register cleanup with resource store
	store := resource.GetStore(ctx)
	if store != nil {
		wrapper.cancelCleanup = store.AddCleanup(func() error {
			if !wrapper.closed {
				if wrapper.cursor != nil {
					wrapper.cursor.Close()
					wrapper.cursor = nil
				}
				if wrapper.query != nil {
					wrapper.query.Close()
					wrapper.query = nil
				}
				wrapper.closed = true
			}
			return nil
		})
	}

	return wrapper
}

// Close marks the query as closed and cancels the cleanup
func (q *QueryWrapper) Close() {
	if !q.closed && q.cancelCleanup != nil {
		q.closed = true
		q.cancelCleanup()
		q.cancelCleanup = nil
	}
}

// Spawn a new Query
func newQuery(l *lua.LState) int {
	languageStr := l.CheckString(1)
	pattern := l.CheckString(2)

	// Spawn language from string
	langInfo := languages.GetLanguageInfo(languageStr)
	if langInfo == nil {
		err := lua.NewLuaError(l, fmt.Sprintf("unsupported language: %s", languageStr)).
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	if langInfo.Language == nil {
		err := lua.NewLuaError(l, fmt.Sprintf("language '%s' does not have a Tree-sitter language binding", languageStr)).
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	lang := treesitter.NewLanguage(langInfo.Language())
	query, queryErr := treesitter.NewQuery(lang, pattern)
	if queryErr != nil {
		err := lua.NewLuaError(l, formatQueryError(queryErr)).
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	ctx := l.Context()
	if ctx == nil {
		err := lua.NewLuaError(l, "no context found").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	queryWrapper := NewQuery(ctx, query, treesitter.NewQueryCursor())
	queryWrapper.source = pattern

	pushQuery(l, queryWrapper)
	return 1
}

func matchToLuaTable(l *lua.LState, query *treesitter.Query, match *treesitter.QueryMatch, source *string) *lua.LTable {
	matchTable := l.CreateTable(0, 3)
	matchTable.RawSetString("id", lua.LNumber(match.Id()))
	matchTable.RawSetString("pattern", lua.LNumber(match.PatternIndex))

	capturesTable := l.CreateTable(len(match.Captures), 0)
	for _, capture := range match.Captures {
		captureTable := l.CreateTable(0, 3)

		pushNode(l, &capture.Node, source)
		nodeUD := l.Get(-1)
		l.Pop(1)

		captureTable.RawSetString("node", nodeUD)
		captureTable.RawSetString("index", lua.LNumber(capture.Index))

		name := query.CaptureNames()[capture.Index]
		if name != "" {
			captureTable.RawSetString("name", lua.LString(name))
		}

		capturesTable.Append(captureTable)
	}
	matchTable.RawSetString("captures", capturesTable)

	return matchTable
}

func queryMatches(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	nodeUD := l.CheckUserData(2)
	source := l.CheckString(3)

	node, ok := nodeUD.Value.(*NodeWrapper)
	if !ok {
		err := lua.NewLuaError(l, "Node expected").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	query.source = source
	matches := query.cursor.Matches(query.query, node.node, []byte(source))

	matchesTable := l.NewTable()
	for match := matches.Next(); match != nil; match = matches.Next() {
		if match.SatisfiesTextPredicate(query.query, nil, nil, []byte(source)) {
			matchTable := matchToLuaTable(l, query.query, match, &query.source)
			matchesTable.Append(matchTable)
		}
	}

	l.Push(matchesTable)
	return 1
}

func queryCaptures(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	nodeUD := l.CheckUserData(2)
	source := l.CheckString(3)

	node, ok := nodeUD.Value.(*NodeWrapper)
	if !ok {
		err := lua.NewLuaError(l, "Node expected").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	query.source = source
	captures := query.cursor.Captures(query.query, node.node, []byte(source))

	capturesTable := l.NewTable()
	for match, index := captures.Next(); match != nil; match, index = captures.Next() {
		if !match.SatisfiesTextPredicate(query.query, nil, nil, []byte(source)) {
			continue
		}

		capture := match.Captures[index]
		captureTable := l.CreateTable(0, 4)

		pushNode(l, &capture.Node, &query.source)
		capturedNodeUD := l.Get(-1)
		l.Pop(1)

		captureTable.RawSetString("node", capturedNodeUD)
		captureTable.RawSetString("index", lua.LNumber(capture.Index))

		name := query.query.CaptureNames()[capture.Index]
		if name != "" {
			captureTable.RawSetString("name", lua.LString(name))
		}

		start := capture.Node.StartByte()
		end := capture.Node.EndByte()
		if end <= uint(len(source)) {
			captureTable.RawSetString("text", lua.LString(source[start:end]))
		}

		capturesTable.Append(captureTable)
	}

	l.Push(capturesTable)
	return 1
}

// Cursor control methods

func querySetByteRange(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	startByte := uint(l.CheckNumber(2))
	endByte := uint(l.CheckNumber(3))
	query.cursor.SetByteRange(startByte, endByte)
	return 0
}

func querySetPointRange(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}

	startPointTbl := l.CheckTable(2)
	startRow := toUint(startPointTbl.RawGetString("row"))
	startCol := toUint(startPointTbl.RawGetString("column"))

	endPointTbl := l.CheckTable(3)
	endRow := toUint(endPointTbl.RawGetString("row"))
	endCol := toUint(endPointTbl.RawGetString("column"))

	startPoint := treesitter.Point{Row: startRow, Column: startCol}
	endPoint := treesitter.Point{Row: endRow, Column: endCol}

	query.cursor.SetPointRange(startPoint, endPoint)
	return 0
}

func querySetMatchLimit(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	limit := uint(l.CheckNumber(2))
	query.cursor.SetMatchLimit(limit)
	return 0
}

func queryGetMatchLimit(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	l.Push(lua.LNumber(query.cursor.MatchLimit()))
	return 1
}

func queryDidExceedMatchLimit(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	l.Push(lua.LBool(query.cursor.DidExceedMatchLimit()))
	return 1
}

func querySetTimeout(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	duration, err := parseDuration(l, 2)
	if err != nil {
		l.ArgError(2, err.Error())
		return 0
	}
	micros := duration.Microseconds()
	if micros < 0 {
		micros = 0
	}
	query.cursor.SetTimeoutMicros(uint64(micros))
	return 0
}

func queryGetTimeout(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	micros := query.cursor.TimeoutMicros()
	l.Push(lua.LNumber(micros * 1000)) // return nanoseconds
	return 1
}

// Pattern and capture control methods

func queryDisablePattern(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	pattern := uint(l.CheckNumber(2))
	query.query.DisablePattern(pattern)
	return 0
}

func queryDisableCapture(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	name := l.CheckString(2)
	query.query.DisableCapture(name)
	return 0
}

func queryIsPatternRooted(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	pattern := uint(l.CheckNumber(2))
	l.Push(lua.LBool(query.query.IsPatternRooted(pattern)))
	return 1
}

func queryIsPatternNonLocal(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	pattern := uint(l.CheckNumber(2))
	l.Push(lua.LBool(query.query.IsPatternNonLocal(pattern)))
	return 1
}

func queryCaptureNameForID(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	id := uint(l.CheckNumber(2))
	names := query.query.CaptureNames()
	if id < uint(len(names)) {
		l.Push(lua.LString(names[id]))
	} else {
		l.Push(lua.LNil)
	}
	return 1
}

func queryCaptureQuantifier(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	pattern := uint(l.CheckNumber(2))
	id := uint(l.CheckNumber(3))

	quantifiers := query.query.CaptureQuantifiers(pattern)
	if id < uint(len(quantifiers)) {
		l.Push(lua.LNumber(quantifiers[id]))
	} else {
		l.Push(lua.LNil)
	}
	return 1
}

// Helper methods

func queryPatternCount(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	l.Push(lua.LNumber(query.query.PatternCount()))
	return 1
}

func queryCaptureCount(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	l.Push(lua.LNumber(len(query.query.CaptureNames())))
	return 1
}

func queryStringCount(l *lua.LState) int {
	// Note: This is a placeholder as the upstream API doesn't expose string count
	l.Push(lua.LNumber(0))
	return 1
}

func queryStartByteForPattern(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	pattern := uint(l.CheckNumber(2))
	l.Push(lua.LNumber(query.query.StartByteForPattern(pattern)))
	return 1
}

// Garbage collection
func queryClose(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*QueryWrapper); ok {
		v.Close()
	}
	return 0
}

// Sets the maximum start depth for query traversal
func querySetMaxStartDepth(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	depth := uint(l.CheckNumber(2))
	query.cursor.SetMaxStartDepth(&depth)
	return 0
}

// Gets property predicates for a given pattern index
func queryGetPropertyPredicates(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	pattern := uint(l.CheckNumber(2))

	predicates := query.query.PropertyPredicates(pattern)
	result := l.NewTable()

	for i, pred := range predicates {
		predTable := l.NewTable()
		predTable.RawSetString("key", lua.LString(pred.Property.Key))
		if pred.Property.Value != nil {
			predTable.RawSetString("value", lua.LString(*pred.Property.Value))
		}
		if pred.Property.CaptureId != nil {
			predTable.RawSetString("capture_id", lua.LNumber(*pred.Property.CaptureId))
		}
		predTable.RawSetString("positive", lua.LBool(pred.Positive))
		result.RawSetInt(i+1, predTable)
	}

	l.Push(result)
	return 1
}

// Gets property settings for a given pattern index
func queryGetPropertySettings(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	pattern := uint(l.CheckNumber(2))

	settings := query.query.PropertySettings(pattern)
	result := l.NewTable()

	for i, setting := range settings {
		settingTable := l.NewTable()
		settingTable.RawSetString("key", lua.LString(setting.Key))
		if setting.Value != nil {
			settingTable.RawSetString("value", lua.LString(*setting.Value))
		}
		if setting.CaptureId != nil {
			settingTable.RawSetString("capture_id", lua.LNumber(*setting.CaptureId))
		}
		result.RawSetInt(i+1, settingTable)
	}

	l.Push(result)
	return 1
}

// Checks if a pattern is guaranteed at a given byte offset
func queryIsPatternGuaranteed(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	byteOffset := uint(l.CheckNumber(2))
	l.Push(lua.LBool(query.query.IsPatternGuaranteedAtStep(byteOffset)))
	return 1
}

// Gets the capture index for a given name
func queryCaptureIndexForName(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	name := l.CheckString(2)
	if index, ok := query.query.CaptureIndexForName(name); ok {
		l.Push(lua.LNumber(index))
		return 1
	}
	l.Push(lua.LNil)
	return 1
}

// Gets the end byte for a given pattern
func queryEndByteForPattern(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	pattern := uint(l.CheckNumber(2))
	l.Push(lua.LNumber(query.query.EndByteForPattern(pattern)))
	return 1
}

// Gets text predicates for a given pattern
func queryGetTextPredicates(l *lua.LState) int {
	query := checkQuery(l)
	if query == nil {
		return 0
	}
	pattern := uint(l.CheckNumber(2))

	predicates := query.query.TextPredicates[pattern]
	result := l.NewTable()

	for i, pred := range predicates {
		predTable := l.NewTable()
		predTable.RawSetString("type", lua.LNumber(pred.Type))
		predTable.RawSetString("capture_id", lua.LNumber(pred.CaptureId))
		predTable.RawSetString("positive", lua.LBool(pred.Positive))
		predTable.RawSetString("match_all_nodes", lua.LBool(pred.MatchAllNodes))

		// Handle different predicate value types
		switch v := pred.Value.(type) {
		case uint:
			predTable.RawSetString("value", lua.LNumber(v))
		case string:
			predTable.RawSetString("value", lua.LString(v))
		case *regexp.Regexp:
			predTable.RawSetString("value", lua.LString(v.String()))
		case []string:
			valueTable := l.NewTable()
			for j, s := range v {
				valueTable.RawSetInt(j+1, lua.LString(s))
			}
			predTable.RawSetString("value", valueTable)
		}

		result.RawSetInt(i+1, predTable)
	}

	l.Push(result)
	return 1
}

// Helper functions

func checkQuery(l *lua.LState) *QueryWrapper {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*QueryWrapper); ok {
		if v.closed {
			l.ArgError(1, "query already closed")
			return nil
		}
		return v
	}
	l.ArgError(1, "Query expected")
	return nil
}

func formatQueryError(err *treesitter.QueryError) string {
	return fmt.Sprintf("%s %s",
		err.Error(),
		err.Message,
	)
}
