package treesitter

import (
	"fmt"
	"regexp"

	"github.com/ponyruntime/pony/internal/closer"
	treesitter "github.com/tree-sitter/go-tree-sitter"
	lua "github.com/yuin/gopher-lua"
)

// QueryWrapper wraps a tree-sitter Query and QueryCursor for Lua integration
type QueryWrapper struct {
	query  *treesitter.Query
	cursor *treesitter.QueryCursor
	source string // Store source text for predicate evaluation
}

// Register the Query type to Lua
func registerQuery(l *lua.LState) {
	mt := l.NewTypeMetatable("treesitter.Query")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		// Core functionality
		"close":                  queryClose,
		"matches":                queryMatches,
		"captures":               queryCaptures,
		"pattern_count":          queryPatternCount,
		"capture_count":          queryCaptureCount,
		"string_count":           queryStringCount,
		"start_byte_for_pattern": queryStartByteForPattern,

		// Cursor control
		"set_byte_range":         querySetByteRange,
		"set_point_range":        querySetPointRange,
		"set_match_limit":        querySetMatchLimit,
		"get_match_limit":        queryGetMatchLimit,
		"did_exceed_match_limit": queryDidExceedMatchLimit,
		"set_timeout":            querySetTimeout,
		"get_timeout":            queryGetTimeout,

		// Pattern/capture control
		"disable_pattern":      queryDisablePattern,
		"disable_capture":      queryDisableCapture,
		"is_pattern_rooted":    queryIsPatternRooted,
		"is_pattern_non_local": queryIsPatternNonLocal,
		"capture_name_for_id":  queryCaptureNameForID,
		"capture_quantifier":   queryCaptureQuantifier,

		"set_max_start_depth":     querySetMaxStartDepth,
		"get_property_predicates": queryGetPropertyPredicates,
		"get_property_settings":   queryGetPropertySettings,
		"is_pattern_guaranteed":   queryIsPatternGuaranteed,
		"capture_index_for_name":  queryCaptureIndexForName,
		"end_byte_for_pattern":    queryEndByteForPattern,
		"get_text_predicates":     queryGetTextPredicates,
	}))
}

// Create a new Query
func newQuery(l *lua.LState) int {
	languageStr := l.CheckString(1)
	pattern := l.CheckString(2)

	// Get language from string
	langInfo := NewLanguages().GetLanguageInfo(languageStr)
	if langInfo == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("unsupported language: %s", languageStr)))
		return 2
	}

	if langInfo.Language == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("language '%s' does not have a Tree-sitter language binding", languageStr)))
		return 2
	}

	lang := treesitter.NewLanguage(langInfo.Language())
	query, err := treesitter.NewQuery(lang, pattern)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(formatQueryError(err)))
		return 2
	}

	// Create query wrapper with cursor
	wrapper := &QueryWrapper{
		query:  query,
		cursor: treesitter.NewQueryCursor(),
	}

	if l.Context() != nil {
		cleanup := closer.FromContext(l.Context())
		if cleanup != nil {
			cleanup.Add(func() error {
				if wrapper.cursor != nil {
					wrapper.cursor.Close()
					wrapper.cursor = nil
				}

				if wrapper.query != nil {
					wrapper.query.Close()
					wrapper.query = nil
				}
				return nil
			})
		}
	}

	ud := l.NewUserData()
	ud.Value = wrapper
	l.SetMetatable(ud, l.GetTypeMetatable("treesitter.Query"))
	l.Push(ud)
	return 1
}

func matchToLuaTable(l *lua.LState, query *treesitter.Query, match *treesitter.QueryMatch, source string) *lua.LTable {
	matchTable := l.NewTable()
	matchTable.RawSetString("id", lua.LNumber(match.Id()))
	matchTable.RawSetString("pattern", lua.LNumber(match.PatternIndex))

	capturesTable := l.NewTable()
	for _, capture := range match.Captures {
		captureTable := l.NewTable()

		// Create Node wrapper for the specific capture's node
		nodeUD := l.NewUserData()
		nodeUD.Value = &NodeWrapper{node: &capture.Node, source: &source}
		l.SetMetatable(nodeUD, l.GetTypeMetatable("treesitter.Node"))

		captureTable.RawSetString("node", nodeUD)
		captureTable.RawSetString("index", lua.LNumber(capture.Index))

		// Get capture name from query pattern
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
	nodeUD := l.CheckUserData(2)
	source := l.CheckString(3)

	node, ok := nodeUD.Value.(*NodeWrapper)
	if !ok {
		l.ArgError(2, "Node expected")
		return 0
	}

	query.source = source
	matches := query.cursor.Matches(query.query, node.node, []byte(source))

	matchesTable := l.NewTable()
	for match := matches.Next(); match != nil; match = matches.Next() {
		if match.SatisfiesTextPredicate(query.query, nil, nil, []byte(source)) {
			matchTable := matchToLuaTable(l, query.query, match, source)
			matchesTable.Append(matchTable)
		}
	}

	l.Push(matchesTable)
	return 1
}

func queryCaptures(l *lua.LState) int {
	query := checkQuery(l)
	nodeUD := l.CheckUserData(2)
	source := l.CheckString(3)

	node, ok := nodeUD.Value.(*NodeWrapper)
	if !ok {
		l.ArgError(2, "Node expected")
		return 0
	}

	query.source = source
	captures := query.cursor.Captures(query.query, node.node, []byte(source))

	capturesTable := l.NewTable()
	for match, index := captures.Next(); match != nil; match, index = captures.Next() {
		if !match.SatisfiesTextPredicate(query.query, nil, nil, []byte(source)) {
			continue
		}

		capture := match.Captures[index]
		captureTable := l.NewTable()

		// Create Node wrapper
		nodeUD := l.NewUserData()
		nodeUD.Value = &NodeWrapper{node: &capture.Node, source: &source}
		l.SetMetatable(nodeUD, l.GetTypeMetatable("treesitter.Node"))

		captureTable.RawSetString("node", nodeUD)
		captureTable.RawSetString("index", lua.LNumber(capture.Index))

		// Add capture name
		name := query.query.CaptureNames()[capture.Index]
		if name != "" {
			captureTable.RawSetString("name", lua.LString(name))
		}

		// Get the text of the captured node
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
	startByte := uint(l.CheckNumber(2))
	endByte := uint(l.CheckNumber(3))
	query.cursor.SetByteRange(startByte, endByte)
	return 0
}

func querySetPointRange(l *lua.LState) int {
	query := checkQuery(l)

	startPointTbl := l.CheckTable(2)
	startRow := uint(startPointTbl.RawGetString("row").(lua.LNumber))
	startCol := uint(startPointTbl.RawGetString("column").(lua.LNumber))

	endPointTbl := l.CheckTable(3)
	endRow := uint(endPointTbl.RawGetString("row").(lua.LNumber))
	endCol := uint(endPointTbl.RawGetString("column").(lua.LNumber))

	startPoint := treesitter.Point{Row: startRow, Column: startCol}
	endPoint := treesitter.Point{Row: endRow, Column: endCol}

	query.cursor.SetPointRange(startPoint, endPoint)
	return 0
}

func querySetMatchLimit(l *lua.LState) int {
	query := checkQuery(l)
	limit := uint(l.CheckNumber(2))
	query.cursor.SetMatchLimit(limit)
	return 0
}

func queryGetMatchLimit(l *lua.LState) int {
	query := checkQuery(l)
	l.Push(lua.LNumber(query.cursor.MatchLimit()))
	return 1
}

func queryDidExceedMatchLimit(l *lua.LState) int {
	query := checkQuery(l)
	l.Push(lua.LBool(query.cursor.DidExceedMatchLimit()))
	return 1
}

func querySetTimeout(l *lua.LState) int {
	query := checkQuery(l)
	timeout := uint64(l.CheckNumber(2))
	query.cursor.SetTimeoutMicros(timeout)
	return 0
}

func queryGetTimeout(l *lua.LState) int {
	query := checkQuery(l)
	l.Push(lua.LNumber(query.cursor.TimeoutMicros()))
	return 1
}

// Pattern and capture control methods

func queryDisablePattern(l *lua.LState) int {
	query := checkQuery(l)
	pattern := uint(l.CheckNumber(2))
	query.query.DisablePattern(pattern)
	return 0
}

func queryDisableCapture(l *lua.LState) int {
	query := checkQuery(l)
	name := l.CheckString(2)
	query.query.DisableCapture(name)
	return 0
}

func queryIsPatternRooted(l *lua.LState) int {
	query := checkQuery(l)
	pattern := uint(l.CheckNumber(2))
	l.Push(lua.LBool(query.query.IsPatternRooted(pattern)))
	return 1
}

func queryIsPatternNonLocal(l *lua.LState) int {
	query := checkQuery(l)
	pattern := uint(l.CheckNumber(2))
	l.Push(lua.LBool(query.query.IsPatternNonLocal(pattern)))
	return 1
}

func queryCaptureNameForID(l *lua.LState) int {
	query := checkQuery(l)
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

func queryPatternCount(L *lua.LState) int {
	query := checkQuery(L)
	L.Push(lua.LNumber(query.query.PatternCount()))
	return 1
}

func queryCaptureCount(L *lua.LState) int {
	query := checkQuery(L)
	L.Push(lua.LNumber(len(query.query.CaptureNames())))
	return 1
}

func queryStringCount(L *lua.LState) int {
	// Note: This is a placeholder as the upstream API doesn't expose string count
	L.Push(lua.LNumber(0))
	return 1
}

func queryStartByteForPattern(L *lua.LState) int {
	query := checkQuery(L)
	pattern := uint(L.CheckNumber(2))
	L.Push(lua.LNumber(query.query.StartByteForPattern(pattern)))
	return 1
}

// Garbage collection
func queryClose(L *lua.LState) int {
	query := checkQuery(L)
	if query.cursor != nil {
		query.cursor.Close()
		query.cursor = nil
	}
	if query.query != nil {
		query.query.Close()
		query.query = nil
	}
	return 0
}

// Sets the maximum start depth for query traversal
func querySetMaxStartDepth(L *lua.LState) int {
	query := checkQuery(L)
	depth := uint(L.CheckNumber(2))
	query.cursor.SetMaxStartDepth(&depth)
	return 0
}

// Gets property predicates for a given pattern index
func queryGetPropertyPredicates(L *lua.LState) int {
	query := checkQuery(L)
	pattern := uint(L.CheckNumber(2))

	predicates := query.query.PropertyPredicates(pattern)
	result := L.NewTable()

	for i, pred := range predicates {
		predTable := L.NewTable()
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

	L.Push(result)
	return 1
}

// Gets property settings for a given pattern index
func queryGetPropertySettings(L *lua.LState) int {
	query := checkQuery(L)
	pattern := uint(L.CheckNumber(2))

	settings := query.query.PropertySettings(pattern)
	result := L.NewTable()

	for i, setting := range settings {
		settingTable := L.NewTable()
		settingTable.RawSetString("key", lua.LString(setting.Key))
		if setting.Value != nil {
			settingTable.RawSetString("value", lua.LString(*setting.Value))
		}
		if setting.CaptureId != nil {
			settingTable.RawSetString("capture_id", lua.LNumber(*setting.CaptureId))
		}
		result.RawSetInt(i+1, settingTable)
	}

	L.Push(result)
	return 1
}

// Checks if a pattern is guaranteed at a given byte offset
func queryIsPatternGuaranteed(L *lua.LState) int {
	query := checkQuery(L)
	byteOffset := uint(L.CheckNumber(2))
	L.Push(lua.LBool(query.query.IsPatternGuaranteedAtStep(byteOffset)))
	return 1
}

// Gets the capture index for a given name
func queryCaptureIndexForName(L *lua.LState) int {
	query := checkQuery(L)
	name := L.CheckString(2)
	if index, ok := query.query.CaptureIndexForName(name); ok {
		L.Push(lua.LNumber(index))
		return 1
	}
	L.Push(lua.LNil)
	return 1
}

// Gets the end byte for a given pattern
func queryEndByteForPattern(L *lua.LState) int {
	query := checkQuery(L)
	pattern := uint(L.CheckNumber(2))
	L.Push(lua.LNumber(query.query.EndByteForPattern(pattern)))
	return 1
}

// Gets text predicates for a given pattern
func queryGetTextPredicates(L *lua.LState) int {
	query := checkQuery(L)
	pattern := uint(L.CheckNumber(2))

	predicates := query.query.TextPredicates[pattern]
	result := L.NewTable()

	for i, pred := range predicates {
		predTable := L.NewTable()
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
			valueTable := L.NewTable()
			for j, s := range v {
				valueTable.RawSetInt(j+1, lua.LString(s))
			}
			predTable.RawSetString("value", valueTable)
		}

		result.RawSetInt(i+1, predTable)
	}

	L.Push(result)
	return 1
}

// Helper functions

func checkQuery(L *lua.LState) *QueryWrapper {
	ud := L.CheckUserData(1)
	if v, ok := ud.Value.(*QueryWrapper); ok {
		return v
	}
	L.ArgError(1, "Query expected")
	return nil
}

func formatQueryError(err *treesitter.QueryError) string {
	return fmt.Sprintf("%s %s",
		err.Error(),
		err.Message,
	)
}
