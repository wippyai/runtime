package text

import (
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/tmc/langchaingo/textsplitter"
	lua "github.com/yuin/gopher-lua"
)

// newMarkdownSplitter creates a new markdown text splitter
func newMarkdownSplitter(l *lua.LState) int {
	var options []textsplitter.Option

	if l.GetTop() > 0 && l.Get(1).Type() == lua.LTTable {
		options = parseMarkdownOptions(l.CheckTable(1))
	}

	// Create the langchain markdown splitter
	splitter := textsplitter.NewMarkdownTextSplitter(options...)

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

// parseMarkdownOptions parses options from a Lua table into langchain options
func parseMarkdownOptions(table *lua.LTable) []textsplitter.Option {
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
		case "code_blocks":
			if b, ok := value.(lua.LBool); ok {
				options = append(options, textsplitter.WithCodeBlocks(bool(b)))
			}
		case "reference_links":
			if b, ok := value.(lua.LBool); ok {
				options = append(options, textsplitter.WithReferenceLinks(bool(b)))
			}
		case "heading_hierarchy":
			if b, ok := value.(lua.LBool); ok {
				options = append(options, textsplitter.WithHeadingHierarchy(bool(b)))
			}
		case "join_table_rows":
			if b, ok := value.(lua.LBool); ok {
				options = append(options, textsplitter.WithJoinTableRows(bool(b)))
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
