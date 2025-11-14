// Package text provides a Lua module for text processing
package text

import (
	"github.com/tmc/langchaingo/textsplitter"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// Module represents the text Lua module
type Module struct{}

// NewTextModule creates a new text module
func NewTextModule() *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "text"
}

// Loader registers the module functions and types
func (m *Module) Loader(l *lua.LState) int {
	// Create main module table
	mod := l.CreateTable(0, 3)

	// Splitter sub-module
	splitterMod := l.CreateTable(0, 2)
	l.SetField(splitterMod, "recursive", l.NewFunction(newRecursiveSplitter))
	l.SetField(splitterMod, "markdown", l.NewFunction(newMarkdownSplitter))
	l.SetField(mod, "splitter", splitterMod)

	// Diff sub-module
	diffMod := l.CreateTable(0, 1)
	l.SetField(diffMod, "new", l.NewFunction(newDiffer))
	l.SetField(mod, "diff", diffMod)

	// Regexp sub-module
	regexpMod := l.CreateTable(0, 1)
	l.SetField(regexpMod, "compile", l.NewFunction(newRegexpCompile))
	l.SetField(mod, "regexp", regexpMod)

	// Register types
	registerTextSplitter(l)
	registerDiffer(l)
	registerRegexp(l)

	l.Push(mod)
	return 1
}

// SplitterWrapper wraps langchain text splitters for Lua
type SplitterWrapper struct {
	splitter textsplitter.TextSplitter
}

// checkSplitter checks if the userdata is a valid SplitterWrapper
func checkSplitter(l *lua.LState, idx int) *SplitterWrapper {
	ud := l.CheckUserData(idx)
	if wrapper, ok := ud.Value.(*SplitterWrapper); ok {
		return wrapper
	}
	l.ArgError(idx, "expected TextSplitter")
	return nil
}

// registerTextSplitter registers the TextSplitter type and its methods
func registerTextSplitter(l *lua.LState) {
	value.RegisterTypeMethods(l, "TextSplitter", nil, map[string]lua.LGFunction{
		"split_text":  splitterSplitText,
		"split_batch": splitterSplitBatch,
	})
}

// splitterSplitText implements the split_text method for TextSplitter objects
func splitterSplitText(l *lua.LState) int {
	wrapper := checkSplitter(l, 1)
	text := l.CheckString(2)

	chunks, err := wrapper.splitter.SplitText(text)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Convert chunks to Lua table
	chunksTable := l.CreateTable(len(chunks), 0)
	for i, chunk := range chunks {
		chunksTable.RawSetInt(i+1, lua.LString(chunk))
	}

	l.Push(chunksTable)
	l.Push(lua.LNil)
	return 2
}

// splitterSplitBatch implements the split_batch method for TextSplitter objects
func splitterSplitBatch(l *lua.LState) int {
	wrapper := checkSplitter(l, 1)
	pagesTable := l.CheckTable(2)

	// Parse pages and keep track of original metadata tables
	var allChunks []lua.LValue

	pagesTable.ForEach(func(_, value lua.LValue) {
		pageTable, ok := value.(*lua.LTable)
		if !ok {
			return
		}

		var content string
		var metaTable *lua.LTable

		pageTable.ForEach(func(key, val lua.LValue) {
			switch key.String() {
			case "content":
				if str, ok := val.(lua.LString); ok {
					content = string(str)
				}
			case "metadata":
				if mt, ok := val.(*lua.LTable); ok {
					metaTable = mt
				}
			}
		})

		if content == "" {
			return
		}

		// Split this page's content
		chunks, err := wrapper.splitter.SplitText(content)
		if err != nil {
			return
		}

		// Create chunk objects with preserved metadata
		for _, chunkText := range chunks {
			chunkTable := l.CreateTable(0, 2)
			chunkTable.RawSetString("content", lua.LString(chunkText))

			if metaTable != nil {
				chunkTable.RawSetString("metadata", metaTable)
			} else {
				chunkTable.RawSetString("metadata", l.CreateTable(0, 0))
			}

			allChunks = append(allChunks, chunkTable)
		}
	})

	// Build result table
	result := l.CreateTable(len(allChunks), 0)
	for i, chunk := range allChunks {
		result.RawSetInt(i+1, chunk)
	}

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}
