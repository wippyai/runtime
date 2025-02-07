package protocol

import (
	tea "github.com/charmbracelet/bubbletea"
	lua "github.com/yuin/gopher-lua"
	"reflect"
)

// TryGetModel attempts to extract a tea.Model from a Lua value.
// It currently handles basic cases, but may support more complex conversions in the future.
func TryGetModel(v lua.LValue) (tea.Model, bool) {
	if model, ok := v.(tea.Model); ok {
		return model, true
	}
	if ud, ok := v.(*lua.LUserData); ok {
		if model, ok := ud.Value.(tea.Model); ok {
			return model, true
		}
	}
	return nil, false
}

// UpdateModelValue attempts to update the model value in a Lua value with a new model.
// It ensures type safety by checking both values are the same concrete type.
// Returns true if update was successful.
func UpdateModelValue(v lua.LValue, newModel tea.Model) bool {
	ud, ok := v.(*lua.LUserData)
	if !ok {
		return false
	}

	currentModel, ok := ud.Value.(tea.Model)
	if !ok {
		return false
	}

	// Assert they're the same concrete type
	if reflect.TypeOf(currentModel) != reflect.TypeOf(newModel) {
		return false
	}

	// Set the new value
	ud.Value = newModel
	return true
}
