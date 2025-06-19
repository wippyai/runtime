package text

import (
	"fmt"
	"regexp"

	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// RegexpWrapper wraps Go's regexp.Regexp for Lua
type RegexpWrapper struct {
	regexp *regexp.Regexp
}

// newRegexpCompile compiles a regex pattern
func newRegexpCompile(l *lua.LState) int {
	pattern := l.CheckString(1)

	re, err := regexp.Compile(pattern)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("regex compile error: %v", err)))
		return 2
	}

	wrapper := &RegexpWrapper{regexp: re}
	ud := l.NewUserData()
	ud.Value = wrapper
	l.SetMetatable(ud, value.GetTypeMetatable(l, "Regexp"))

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

// checkRegexp validates the regexp userdata
func checkRegexp(l *lua.LState) *RegexpWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*RegexpWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected Regexp")
	return nil
}

// registerRegexp registers the Regexp type and methods
func registerRegexp(l *lua.LState) {
	value.RegisterTypeMethods(l, "Regexp", nil, map[string]lua.LGFunction{
		"find_all_string_submatch": regexpFindAllStringSubmatch,
		"find_string_submatch":     regexpFindStringSubmatch,
		"find_all_string":          regexpFindAllString,
		"find_string":              regexpFindString,
		"find_all_string_index":    regexpFindAllStringIndex,
		"find_string_index":        regexpFindStringIndex,
		"replace_all_string":       regexpReplaceAllString,
		"match_string":             regexpMatchString,
		"split":                    regexpSplit,
		"num_subexp":               regexpNumSubexp,
		"subexp_names":             regexpSubexpNames,
		"string":                   regexpString,
	})
}

// Core matching methods

func regexpFindAllStringSubmatch(l *lua.LState) int {
	wrapper := checkRegexp(l)
	content := l.CheckString(2)
	matches := wrapper.regexp.FindAllStringSubmatch(content, -1)

	result := l.CreateTable(len(matches), 0)
	for i, match := range matches {
		matchTable := l.CreateTable(len(match), 0)
		for j, submatch := range match {
			matchTable.RawSetInt(j+1, lua.LString(submatch))
		}
		result.RawSetInt(i+1, matchTable)
	}
	l.Push(result)
	return 1
}

func regexpFindStringSubmatch(l *lua.LState) int {
	wrapper := checkRegexp(l)
	content := l.CheckString(2)
	match := wrapper.regexp.FindStringSubmatch(content)

	if match == nil {
		l.Push(lua.LNil)
		return 1
	}

	result := l.CreateTable(len(match), 0)
	for i, submatch := range match {
		result.RawSetInt(i+1, lua.LString(submatch))
	}
	l.Push(result)
	return 1
}

func regexpFindAllString(l *lua.LState) int {
	wrapper := checkRegexp(l)
	content := l.CheckString(2)
	matches := wrapper.regexp.FindAllString(content, -1)

	result := l.CreateTable(len(matches), 0)
	for i, match := range matches {
		result.RawSetInt(i+1, lua.LString(match))
	}
	l.Push(result)
	return 1
}

func regexpFindString(l *lua.LState) int {
	wrapper := checkRegexp(l)
	content := l.CheckString(2)
	match := wrapper.regexp.FindString(content)

	if match == "" {
		l.Push(lua.LNil)
	} else {
		l.Push(lua.LString(match))
	}
	return 1
}

// Index methods - return positions
func regexpFindAllStringIndex(l *lua.LState) int {
	wrapper := checkRegexp(l)
	content := l.CheckString(2)
	indices := wrapper.regexp.FindAllStringIndex(content, -1)

	if indices == nil {
		l.Push(lua.LNil)
		return 1
	}

	result := l.CreateTable(len(indices), 0)
	for i, index := range indices {
		indexTable := l.CreateTable(2, 0)
		// Go: [start, end) 0-based -> Lua: [start, end] 1-based inclusive
		indexTable.RawSetInt(1, lua.LNumber(index[0]+1)) // start position
		indexTable.RawSetInt(2, lua.LNumber(index[1]))   // end position (Go's exclusive end becomes Lua's inclusive end)
		result.RawSetInt(i+1, indexTable)
	}
	l.Push(result)
	return 1
}

func regexpFindStringIndex(l *lua.LState) int {
	wrapper := checkRegexp(l)
	content := l.CheckString(2)
	index := wrapper.regexp.FindStringIndex(content)

	if index == nil {
		l.Push(lua.LNil)
		return 1
	}

	result := l.CreateTable(2, 0)
	// Go: [start, end) 0-based -> Lua: [start, end] 1-based inclusive
	result.RawSetInt(1, lua.LNumber(index[0]+1)) // start position
	result.RawSetInt(2, lua.LNumber(index[1]))   // end position (Go's exclusive end becomes Lua's inclusive end)
	l.Push(result)
	return 1
}

// Text manipulation methods
func regexpReplaceAllString(l *lua.LState) int {
	wrapper := checkRegexp(l)
	content := l.CheckString(2)
	replacement := l.CheckString(3)
	result := wrapper.regexp.ReplaceAllString(content, replacement)
	l.Push(lua.LString(result))
	return 1
}

func regexpMatchString(l *lua.LState) int {
	wrapper := checkRegexp(l)
	content := l.CheckString(2)
	matches := wrapper.regexp.MatchString(content)
	l.Push(lua.LBool(matches))
	return 1
}

func regexpSplit(l *lua.LState) int {
	wrapper := checkRegexp(l)
	content := l.CheckString(2)
	n := int(l.OptNumber(3, -1))
	parts := wrapper.regexp.Split(content, n)

	result := l.CreateTable(len(parts), 0)
	for i, part := range parts {
		result.RawSetInt(i+1, lua.LString(part))
	}
	l.Push(result)
	return 1
}

// Introspection methods
func regexpNumSubexp(l *lua.LState) int {
	wrapper := checkRegexp(l)
	l.Push(lua.LNumber(wrapper.regexp.NumSubexp()))
	return 1
}

func regexpSubexpNames(l *lua.LState) int {
	wrapper := checkRegexp(l)
	names := wrapper.regexp.SubexpNames()

	result := l.CreateTable(len(names), 0)
	for i, name := range names {
		result.RawSetInt(i+1, lua.LString(name)) // Keep empty strings as empty strings
	}
	l.Push(result)
	return 1
}

func regexpString(l *lua.LState) int {
	wrapper := checkRegexp(l)
	l.Push(lua.LString(wrapper.regexp.String()))
	return 1
}
