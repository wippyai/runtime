package render

import (
	"github.com/charmbracelet/bubbles/runeutil"
	"github.com/charmbracelet/lipgloss"
	lua "github.com/yuin/gopher-lua"
)

// Position constants for join operations
const (
	Top    = 0.0
	Center = 0.5
	Bottom = 1.0
	Left   = 0.0
	Right  = 1.0
)

// RegisterTextUtils registers size and text manipulation related helper functions
func RegisterTextUtils(l *lua.LState, mod *lua.LTable) {
	// Create text utils table
	utilsTbl := l.NewTable()

	// Size functions
	l.SetField(utilsTbl, "width", l.NewFunction(func(l *lua.LState) int {
		str := l.CheckString(1)
		width := lipgloss.Width(str)
		l.Push(lua.LNumber(width))
		return 1
	}))

	l.SetField(utilsTbl, "height", l.NewFunction(func(l *lua.LState) int {
		str := l.CheckString(1)
		height := lipgloss.Height(str)
		l.Push(lua.LNumber(height))
		return 1
	}))

	l.SetField(utilsTbl, "size", l.NewFunction(func(l *lua.LState) int {
		str := l.CheckString(1)
		width, height := lipgloss.Size(str)
		l.Push(lua.LNumber(width))
		l.Push(lua.LNumber(height))
		return 2
	}))

	l.SetField(utilsTbl, "max_width", l.NewFunction(func(l *lua.LState) int {
		tbl := l.CheckTable(1)
		maxWidth := 0

		tbl.ForEach(func(_ lua.LValue, value lua.LValue) {
			if str, ok := value.(lua.LString); ok {
				width := lipgloss.Width(string(str))
				if width > maxWidth {
					maxWidth = width
				}
			}
		})

		l.Push(lua.LNumber(maxWidth))
		return 1
	}))

	l.SetField(utilsTbl, "max_height", l.NewFunction(func(l *lua.LState) int {
		tbl := l.CheckTable(1)
		maxHeight := 0

		tbl.ForEach(func(_ lua.LValue, value lua.LValue) {
			if str, ok := value.(lua.LString); ok {
				height := lipgloss.Height(string(str))
				if height > maxHeight {
					maxHeight = height
				}
			}
		})

		l.Push(lua.LNumber(maxHeight))
		return 1
	}))

	// Join functions
	l.SetField(utilsTbl, "join_horizontal", l.NewFunction(func(l *lua.LState) int {
		pos := float64(l.CheckNumber(1))
		strs := make([]string, 0, l.GetTop()-1)

		for i := 2; i <= l.GetTop(); i++ {
			strs = append(strs, l.CheckString(i))
		}

		result := lipgloss.JoinHorizontal(lipgloss.Position(pos), strs...)
		l.Push(lua.LString(result))
		return 1
	}))

	l.SetField(utilsTbl, "join_vertical", l.NewFunction(func(l *lua.LState) int {
		pos := float64(l.CheckNumber(1))
		strs := make([]string, 0, l.GetTop()-1)

		for i := 2; i <= l.GetTop(); i++ {
			strs = append(strs, l.CheckString(i))
		}

		result := lipgloss.JoinVertical(lipgloss.Position(pos), strs...)
		l.Push(lua.LString(result))
		return 1
	}))

	// Style runes function
	l.SetField(utilsTbl, "style_runes", l.NewFunction(func(l *lua.LState) int {
		str := l.CheckString(1)
		indicesTable := l.CheckTable(2)

		// Convert Lua table of indices to Go slice
		indices := make([]int, 0)
		indicesTable.ForEach(func(_ lua.LValue, v lua.LValue) {
			if num, ok := v.(lua.LNumber); ok {
				indices = append(indices, int(num))
			}
		})

		// Get matched and unmatched styles
		matched, ok1 := l.CheckUserData(3).Value.(*Style)
		unmatched, ok2 := l.CheckUserData(4).Value.(*Style)

		if !ok1 || !ok2 {
			l.ArgError(3, "Style expected for matched and unmatched parameters")
			return 0
		}

		result := lipgloss.StyleRunes(str, indices, matched.Style, unmatched.Style)
		l.Push(lua.LString(result))
		return 1
	}))

	l.SetField(utilsTbl, "sanitize_runes", l.NewFunction(func(l *lua.LState) int {
		str := l.CheckString(1)
		nlRepl := "\n"
		tabRepl := "    "

		if l.GetTop() > 1 {
			nlRepl = l.OptString(2, "\n")
		}
		if l.GetTop() > 2 {
			tabRepl = l.OptString(3, "    ")
		}

		sanitizer := runeutil.NewSanitizer(
			runeutil.ReplaceNewlines(nlRepl),
			runeutil.ReplaceTabs(tabRepl),
		)

		result := sanitizer.Sanitize([]rune(str))
		l.Push(lua.LString(string(result)))
		return 1
	}))

	// Position constants
	posTbl := l.NewTable()
	l.SetField(posTbl, "TOP", lua.LNumber(Top))
	l.SetField(posTbl, "CENTER", lua.LNumber(Center))
	l.SetField(posTbl, "BOTTOM", lua.LNumber(Bottom))
	l.SetField(posTbl, "LEFT", lua.LNumber(Left))
	l.SetField(posTbl, "RIGHT", lua.LNumber(Right))
	l.SetField(utilsTbl, "position", posTbl)

	// Set the utils table in the module
	l.SetField(mod, "text", utilsTbl)
}
