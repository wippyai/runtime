package _redo_

//
//// TryGetModel attempts to convert a Lua value to a tea.Model.
//// It first checks if the value itself implements tea.Model,
//// then if it is a userdata wrapping a tea.Model.
//func TryGetModel(l *lua.LState, v lua.LValue) (tea.Model, bool) {
//	if model, ok := v.(tea.Model); ok {
//		return model, true
//	}
//	if ud, ok := v.(*lua.LUserData); ok {
//		if model, ok := ud.Value.(tea.Model); ok {
//			return model, true
//		}
//	}
//	return nil, false
//}
//
//// WrapModel converts a tea.Model back to a Lua value.
//// It uses the metatable from the original Lua value (if any)
//// so that each model retains its original type (for example, "btea.TextArea")
//// rather than defaulting to a generic "btea.Model".
//func WrapModel(l *lua.LState, orig lua.LValue, m tea.Model) lua.LValue {
//	// If the new model already implements lua.LValue (e.g. a Lua table), return it.
//	if lv, ok := m.(lua.LValue); ok {
//		return lv
//	}
//
//	// Attempt to reuse the original userdata's metatable.
//	var mt *lua.LTable
//	if ud, ok := orig.(*lua.LUserData); ok && ud.Metatable != nil {
//		mt = ud.Metatable
//	}
//
//	// Wrap the updated model in a new userdata with the chosen metatable.
//	newUD := l.NewUserData()
//	newUD.Value = m
//	l.SetMetatable(newUD, mt)
//	return newUD
//}
