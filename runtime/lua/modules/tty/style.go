// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"github.com/charmbracelet/lipgloss"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const styleTypeName = "tty.Style"

func init() {
	value.RegisterTypeMethods(nil, styleTypeName,
		map[string]lua.LGoFunc{"__tostring": styleToString},
		map[string]lua.LGoFunc{
			"render":            styleRender,
			"foreground":        styleForeground,
			"background":        styleBackground,
			"bold":              styleBold,
			"italic":            styleItalic,
			"underline":         styleUnderline,
			"strikethrough":     styleStrikethrough,
			"faint":             styleFaint,
			"blink":             styleBlink,
			"reverse":           styleReverse,
			"padding":           stylePadding,
			"margin":            styleMargin,
			"border":            styleBorder,
			"border_foreground": styleBorderForeground,
			"border_background": styleBorderBackground,
			"width":             styleWidth,
			"height":            styleHeight,
			"max_width":         styleMaxWidth,
			"max_height":        styleMaxHeight,
			"align":             styleAlign,
			"align_vertical":    styleAlignVertical,
			"inline":            styleInline,
			"copy":              styleCopy,
		})
}

type styleWrapper struct {
	style lipgloss.Style
}

func checkStyle(l *lua.LState) *styleWrapper {
	ud := l.CheckUserData(1)
	if s, ok := ud.Value.(*styleWrapper); ok {
		return s
	}
	l.ArgError(1, "tty.Style expected")
	return nil
}

func pushStyle(l *lua.LState, s *styleWrapper) *lua.LUserData {
	return value.PushTypedUserData(l, s, styleTypeName)
}

func ttyStyleNew(l *lua.LState) int {
	s := &styleWrapper{style: lipgloss.NewStyle()}
	pushStyle(l, s)
	return 1
}

func styleToString(l *lua.LState) int {
	l.Push(lua.LString("tty.Style{}"))
	return 1
}

func styleRender(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	n := l.GetTop()
	strs := make([]string, 0, n-1)
	for i := 2; i <= n; i++ {
		strs = append(strs, l.ToString(i))
	}
	result := s.style.Render(strs...)
	l.Push(lua.LString(result))
	return 1
}

func pushDerived(l *lua.LState, s lipgloss.Style) int {
	pushStyle(l, &styleWrapper{style: s})
	return 1
}

func styleForeground(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.Foreground(lipgloss.Color(l.CheckString(2))))
}

func styleBackground(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.Background(lipgloss.Color(l.CheckString(2))))
}

func styleBold(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.Bold(l.OptBool(2, true)))
}

func styleItalic(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.Italic(l.OptBool(2, true)))
}

func styleUnderline(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.Underline(l.OptBool(2, true)))
}

func styleStrikethrough(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.Strikethrough(l.OptBool(2, true)))
}

func styleFaint(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.Faint(l.OptBool(2, true)))
}

func styleBlink(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.Blink(l.OptBool(2, true)))
}

func styleReverse(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.Reverse(l.OptBool(2, true)))
}

func stylePadding(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.Padding(readSpacingArgs(l, 2)...))
}

func styleMargin(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.Margin(readSpacingArgs(l, 2)...))
}

func styleBorder(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	border := resolveBorder(l.CheckString(2))
	sides := make([]bool, 0, 4)
	for i := 3; i <= l.GetTop(); i++ {
		sides = append(sides, l.ToBool(i))
	}
	return pushDerived(l, s.style.Border(border, sides...))
}

func styleBorderForeground(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.BorderForeground(readColorArgs(l, 2)...))
}

func styleBorderBackground(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.BorderBackground(readColorArgs(l, 2)...))
}

func styleWidth(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.Width(l.CheckInt(2)))
}

func styleHeight(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.Height(l.CheckInt(2)))
}

func styleMaxWidth(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.MaxWidth(l.CheckInt(2)))
}

func styleMaxHeight(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.MaxHeight(l.CheckInt(2)))
}

func styleAlign(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.Align(lipgloss.Position(l.CheckNumber(2))))
}

func styleAlignVertical(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.AlignVertical(lipgloss.Position(l.CheckNumber(2))))
}

func styleInline(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	return pushDerived(l, s.style.Inline(l.OptBool(2, true)))
}

func styleCopy(l *lua.LState) int {
	s := checkStyle(l)
	if s == nil {
		return 0
	}
	cp := &styleWrapper{style: s.style}
	pushStyle(l, cp)
	return 1
}

func resolveBorder(name string) lipgloss.Border {
	switch name {
	case "normal":
		return lipgloss.NormalBorder()
	case "rounded":
		return lipgloss.RoundedBorder()
	case "thick":
		return lipgloss.ThickBorder()
	case "double":
		return lipgloss.DoubleBorder()
	case "hidden":
		return lipgloss.HiddenBorder()
	default:
		return lipgloss.NormalBorder()
	}
}

func readSpacingArgs(l *lua.LState, startIdx int) []int {
	n := l.GetTop()
	values := make([]int, 0, n-startIdx+1)
	for i := startIdx; i <= n; i++ {
		values = append(values, l.CheckInt(i))
	}
	return values
}

func readColorArgs(l *lua.LState, startIdx int) []lipgloss.TerminalColor {
	n := l.GetTop()
	colors := make([]lipgloss.TerminalColor, 0, n-startIdx+1)
	for i := startIdx; i <= n; i++ {
		colors = append(colors, lipgloss.Color(l.CheckString(i)))
	}
	return colors
}
