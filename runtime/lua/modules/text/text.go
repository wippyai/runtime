// Package text provides a Lua module for text processing
package text

import (
	"sync"

	"github.com/tmc/langchaingo/textsplitter"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// Module represents the text Lua module
type Module struct {
	once        sync.Once
	moduleTable *lua.LTable
}

// NewTextModule creates a new text module
func NewTextModule() *Module {
	return &Module{}
}

func (m *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "text",
		Description: "Text processing, splitting, diff, and regex",
		Class:       []string{luaapi.ClassDeterministic},
	}
}

// Loader registers the module functions and types
func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		mod := l.CreateTable(0, 3)

		splitterMod := l.CreateTable(0, 2)
		splitterMod.RawSetString("recursive", l.NewFunction(newRecursiveSplitter))
		splitterMod.RawSetString("markdown", l.NewFunction(newMarkdownSplitter))
		mod.RawSetString("splitter", splitterMod)

		diffMod := l.CreateTable(0, 1)
		diffMod.RawSetString("new", l.NewFunction(newDiffer))
		mod.RawSetString("diff", diffMod)

		regexpMod := l.CreateTable(0, 1)
		regexpMod.RawSetString("compile", l.NewFunction(newRegexpCompile))
		mod.RawSetString("regexp", regexpMod)

		registerTextSplitter(l)
		registerDiffer(l)
		registerRegexp(l)
		mod.Immutable = true
		m.moduleTable = mod
	})
	l.Push(m.moduleTable)
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
		l.Push(newTextOperationError(l, err, "split_text"))
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
