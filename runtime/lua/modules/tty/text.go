// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"github.com/charmbracelet/lipgloss"
	lua "github.com/wippyai/go-lua"
)

// textWidth returns the printable width of a string (ANSI-aware).
func textWidth(l *lua.LState) int {
	s := l.CheckString(1)
	l.Push(lua.LNumber(lipgloss.Width(s)))
	return 1
}

// textHeight returns the number of lines in a string.
func textHeight(l *lua.LState) int {
	s := l.CheckString(1)
	l.Push(lua.LNumber(lipgloss.Height(s)))
	return 1
}

// textSize returns width and height of a string.
func textSize(l *lua.LState) int {
	s := l.CheckString(1)
	w, h := lipgloss.Size(s)
	l.Push(lua.LNumber(w))
	l.Push(lua.LNumber(h))
	return 2
}

// textJoinHorizontal joins strings horizontally at a given vertical position.
func textJoinHorizontal(l *lua.LState) int {
	pos := lipgloss.Position(l.CheckNumber(1))
	strs := collectStrings(l, 2)
	result := lipgloss.JoinHorizontal(pos, strs...)
	l.Push(lua.LString(result))
	return 1
}

// textJoinVertical joins strings vertically at a given horizontal position.
func textJoinVertical(l *lua.LState) int {
	pos := lipgloss.Position(l.CheckNumber(1))
	strs := collectStrings(l, 2)
	result := lipgloss.JoinVertical(pos, strs...)
	l.Push(lua.LString(result))
	return 1
}

// textMaxWidth returns the maximum printable width across a table of strings.
func textMaxWidth(l *lua.LState) int {
	tbl := l.CheckTable(1)
	max := 0
	tbl.ForEach(func(_, v lua.LValue) {
		if s, ok := v.(lua.LString); ok {
			w := lipgloss.Width(string(s))
			if w > max {
				max = w
			}
		}
	})
	l.Push(lua.LNumber(max))
	return 1
}

// textMaxHeight returns the maximum height across a table of strings.
func textMaxHeight(l *lua.LState) int {
	tbl := l.CheckTable(1)
	max := 0
	tbl.ForEach(func(_, v lua.LValue) {
		if s, ok := v.(lua.LString); ok {
			h := lipgloss.Height(string(s))
			if h > max {
				max = h
			}
		}
	})
	l.Push(lua.LNumber(max))
	return 1
}

// textPlace places a string within a box of given dimensions.
// place(width, height, hPos, vPos, str)
func textPlace(l *lua.LState) int {
	width := l.CheckInt(1)
	height := l.CheckInt(2)
	hPos := lipgloss.Position(l.CheckNumber(3))
	vPos := lipgloss.Position(l.CheckNumber(4))
	str := l.CheckString(5)
	result := lipgloss.Place(width, height, hPos, vPos, str)
	l.Push(lua.LString(result))
	return 1
}

// textPlaceHorizontal centers a string horizontally within a given width.
// place_horizontal(width, pos, str)
func textPlaceHorizontal(l *lua.LState) int {
	width := l.CheckInt(1)
	pos := lipgloss.Position(l.CheckNumber(2))
	str := l.CheckString(3)
	result := lipgloss.PlaceHorizontal(width, pos, str)
	l.Push(lua.LString(result))
	return 1
}

// textPlaceVertical centers a string vertically within a given height.
// place_vertical(height, pos, str)
func textPlaceVertical(l *lua.LState) int {
	height := l.CheckInt(1)
	pos := lipgloss.Position(l.CheckNumber(2))
	str := l.CheckString(3)
	result := lipgloss.PlaceVertical(height, pos, str)
	l.Push(lua.LString(result))
	return 1
}

func collectStrings(l *lua.LState, startIdx int) []string {
	n := l.GetTop()
	strs := make([]string, 0, n-startIdx+1)
	for i := startIdx; i <= n; i++ {
		strs = append(strs, l.ToString(i))
	}
	return strs
}
