package models

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	lua "github.com/yuin/gopher-lua"
)

// ModelWrapper provides common functionality for all bubble tea model wrappers
type ModelWrapper interface {
	tea.Model
	Model() any
}

// HandleModelUpdate is a generic helper for model updates
// It handles:
// 1. Model type assertions and updates
// 2. Command wrapping
// 3. Lua stack management
func HandleModelUpdate[T any](l *lua.LState, wrapper ModelWrapper, newModel tea.Model, cmd tea.Cmd) int {
	// Update model if types match
	if m, ok := newModel.(T); ok {
		if target, ok := wrapper.Model().(*T); ok {
			*target = m
		}
	}

	// Return command if present
	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
	}
	return 0
}
