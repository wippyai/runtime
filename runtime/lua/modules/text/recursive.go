package text

import (
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/tmc/langchaingo/textsplitter"
	lua "github.com/yuin/gopher-lua"
)

// newRecursiveSplitter creates a new recursive character splitter
func newRecursiveSplitter(l *lua.LState) int {
	var options []textsplitter.Option

	if l.GetTop() > 0 && l.Get(1).Type() == lua.LTTable {
		options = parseRecursiveOptions(l.CheckTable(1))
	}

	// Create the langchain recursive splitter
	splitter := textsplitter.NewRecursiveCharacter(options...)

	// Wrap it
	wrapper := &TextSplitterWrapper{
		splitter: splitter,
	}

	// Create userdata and set metatable
	ud := l.NewUserData()
	ud.Value = wrapper
	l.SetMetatable(ud, value.GetTypeMetatable(l, "TextSplitter"))

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

// parseRecursiveOptions parses options from a Lua table into langchain options
func parseRecursiveOptions(table *lua.LTable) []textsplitter.Option {
	var options []textsplitter.Option

	table.ForEach(func(key, value lua.LValue) {
		switch key.String() {
		case "chunk_size":
			if num, ok := value.(lua.LNumber); ok {
				options = append(options, textsplitter.WithChunkSize(int(num)))
			}
		case "chunk_overlap":
			if num, ok := value.(lua.LNumber); ok {
				options = append(options, textsplitter.WithChunkOverlap(int(num)))
			}
		case "keep_separator":
			if b, ok := value.(lua.LBool); ok {
				options = append(options, textsplitter.WithKeepSeparator(bool(b)))
			}
		case "separators":
			if tbl, ok := value.(*lua.LTable); ok {
				separators := parseSeparatorsTable(tbl)
				if len(separators) > 0 {
					options = append(options, textsplitter.WithSeparators(separators))
				}
			}
		}
	})

	return options
}

// parseSeparatorsTable converts a Lua table to a string slice
func parseSeparatorsTable(table *lua.LTable) []string {
	var separators []string
	table.ForEach(func(_, value lua.LValue) {
		if str, ok := value.(lua.LString); ok {
			separators = append(separators, string(str))
		}
	})
	return separators
}
