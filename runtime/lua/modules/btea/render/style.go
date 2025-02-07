package render

import (
	"github.com/charmbracelet/lipgloss"
	lua "github.com/yuin/gopher-lua"
)

// Style wraps lipgloss.Style for Lua
type Style struct {
	Style lipgloss.Style
}

// RegisterStyle registers the Style component
func RegisterStyle(l *lua.LState, mod *lua.LTable) {
	// Create and register the Style metatable
	mt := l.NewTypeMetatable("btea.Style")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"render":        styleRender,
		"foreground":    styleForeground,
		"background":    styleBackground,
		"bold":          styleBold,
		"italic":        styleItalic,
		"underline":     styleUnderline,
		"strikethrough": styleStrikethrough,
		"faint":         styleFaint,
		"blink":         styleBlink,
		"reverse":       styleReverse,
		"padding":       stylePadding,
		"margin":        styleMargin,
		"border":        styleBorder,
		"custom_border": styleCustomBorder,
		"width":         styleWidth,
		"height":        styleHeight,
		"align":         styleAlign,
		"inline":        styleInline,
		"max_width":     styleMaxWidth,
		"max_height":    styleMaxHeight,
		"tab_width":     styleTabWidth,
		"copy":          styleCopy,
		"inherit":       styleInherit,

		// Border styling
		"border_foreground": styleBorderForeground,
		"border_background": styleBorderBackground,

		// Individual border edge styling
		"border_top_foreground":    styleBorderTopForeground,
		"border_bottom_foreground": styleBorderBottomForeground,
		"border_left_foreground":   styleBorderLeftForeground,
		"border_right_foreground":  styleBorderRightForeground,

		"border_top_background":    styleBorderTopBackground,
		"border_bottom_background": styleBorderBottomBackground,
		"border_left_background":   styleBorderLeftBackground,
		"border_right_background":  styleBorderRightBackground,

		// Alignment
		"align_vertical": styleAlignVertical,

		// Space handling
		"underline_spaces":     styleUnderlineSpaces,
		"strikethrough_spaces": styleStrikethroughSpaces,

		// Margin background
		"margin_background": styleMarginBackground,

		// String operations
		"set_string": styleSetString,
		"get_value":  styleGetValue,
	}))

	// Register constructor
	l.SetField(mod, "new_style", l.NewFunction(newStyle))

	// Register border constants
	bordersTbl := l.NewTable()
	l.SetField(bordersTbl, "NORMAL", lua.LString("normal"))
	l.SetField(bordersTbl, "ROUNDED", lua.LString("rounded"))
	l.SetField(bordersTbl, "THICK", lua.LString("thick"))
	l.SetField(bordersTbl, "DOUBLE", lua.LString("double"))
	l.SetField(mod, "borders", bordersTbl)

	// Register alignment constants
	alignTbl := l.NewTable()
	l.SetField(alignTbl, "LEFT", lua.LNumber(lipgloss.Left))
	l.SetField(alignTbl, "CENTER", lua.LNumber(lipgloss.Center))
	l.SetField(alignTbl, "RIGHT", lua.LNumber(lipgloss.Right))
	l.SetField(mod, "align", alignTbl)
}

func newStyle(l *lua.LState) int {
	ud := l.NewUserData()
	ud.Value = &Style{Style: lipgloss.NewStyle()}
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

// CheckStyle checks and returns a Style from the Lua state at the given index
func CheckStyle(l *lua.LState, index int) *Style {
	ud := l.CheckUserData(index)
	if v, ok := ud.Value.(*Style); ok {
		return v
	}
	l.ArgError(index, "Style expected")
	return nil
}

// ToStyle converts a Lua value to a Style if possible
func ToStyle(value lua.LValue) (*Style, bool) {
	if ud, ok := value.(*lua.LUserData); ok {
		if style, ok := ud.Value.(*Style); ok {
			return style, true
		}
	}
	return nil, false
}

// PushStyle creates a new Style userdata and pushes it onto the stack
func PushStyle(l *lua.LState, style *Style) {
	ud := l.NewUserData()
	ud.Value = style
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
}

func styleRender(l *lua.LState) int {
	s := CheckStyle(l, 1)
	str := l.CheckString(2)
	l.Push(lua.LString(s.Style.Render(str)))
	return 1
}

func styleForeground(l *lua.LState) int {
	s := CheckStyle(l, 1)
	color := l.CheckString(2)
	newStyle := &Style{Style: s.Style.Foreground(lipgloss.Color(color))}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleBackground(l *lua.LState) int {
	s := CheckStyle(l, 1)
	color := l.CheckString(2)
	newStyle := &Style{Style: s.Style.Background(lipgloss.Color(color))}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleBold(l *lua.LState) int {
	s := CheckStyle(l, 1)
	newStyle := &Style{Style: s.Style.Bold(true)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleItalic(l *lua.LState) int {
	s := CheckStyle(l, 1)
	newStyle := &Style{Style: s.Style.Italic(true)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleUnderline(l *lua.LState) int {
	s := CheckStyle(l, 1)
	newStyle := &Style{Style: s.Style.Underline(true)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleStrikethrough(l *lua.LState) int {
	s := CheckStyle(l, 1)
	newStyle := &Style{Style: s.Style.Strikethrough(true)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleFaint(l *lua.LState) int {
	s := CheckStyle(l, 1)
	newStyle := &Style{Style: s.Style.Faint(true)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleBlink(l *lua.LState) int {
	s := CheckStyle(l, 1)
	newStyle := &Style{Style: s.Style.Blink(true)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleReverse(l *lua.LState) int {
	s := CheckStyle(l, 1)
	newStyle := &Style{Style: s.Style.Reverse(true)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func stylePadding(l *lua.LState) int {
	s := CheckStyle(l, 1)
	top := l.CheckInt(2)
	right := l.OptInt(3, top)
	bottom := l.OptInt(4, top)
	left := l.OptInt(5, right)
	newStyle := &Style{Style: s.Style.Padding(top, right, bottom, left)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleMargin(l *lua.LState) int {
	s := CheckStyle(l, 1)
	top := l.CheckInt(2)
	right := l.OptInt(3, top)
	bottom := l.OptInt(4, top)
	left := l.OptInt(5, right)
	newStyle := &Style{Style: s.Style.Margin(top, right, bottom, left)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleBorder(l *lua.LState) int {
	s := CheckStyle(l, 1)
	styleStr := l.CheckString(2)
	var border lipgloss.Border
	switch styleStr {
	case "normal":
		border = lipgloss.NormalBorder()
	case "rounded":
		border = lipgloss.RoundedBorder()
	case "thick":
		border = lipgloss.ThickBorder()
	case "double":
		border = lipgloss.DoubleBorder()
	default:
		border = lipgloss.NormalBorder()
	}
	newStyle := &Style{Style: s.Style.Border(border)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleCustomBorder(l *lua.LState) int {
	s := CheckStyle(l, 1)
	borderTbl := l.CheckTable(2)

	border := lipgloss.Border{}

	borderTbl.ForEach(func(key, value lua.LValue) {
		keyStr := lua.LVAsString(key)
		valStr := lua.LVAsString(value)
		switch keyStr {
		case "top":
			border.Top = valStr
		case "bottom":
			border.Bottom = valStr
		case "left":
			border.Left = valStr
		case "right":
			border.Right = valStr
		case "top_left":
			border.TopLeft = valStr
		case "top_right":
			border.TopRight = valStr
		case "bottom_left":
			border.BottomLeft = valStr
		case "bottom_right":
			border.BottomRight = valStr
		}
	})

	newStyle := &Style{Style: s.Style.Border(border)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleWidth(l *lua.LState) int {
	s := CheckStyle(l, 1)
	width := l.CheckInt(2)
	newStyle := &Style{Style: s.Style.Width(width)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleHeight(l *lua.LState) int {
	s := CheckStyle(l, 1)
	height := l.CheckInt(2)
	newStyle := &Style{Style: s.Style.Height(height)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleAlign(l *lua.LState) int {
	s := CheckStyle(l, 1)
	align := lipgloss.Position(l.CheckInt(2))
	newStyle := &Style{Style: s.Style.Align(align)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleInline(l *lua.LState) int {
	s := CheckStyle(l, 1)
	inline := l.CheckBool(2)
	newStyle := &Style{Style: s.Style.Inline(inline)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleMaxWidth(l *lua.LState) int {
	s := CheckStyle(l, 1)
	width := l.CheckInt(2)
	newStyle := &Style{Style: s.Style.MaxWidth(width)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleMaxHeight(l *lua.LState) int {
	s := CheckStyle(l, 1)
	height := l.CheckInt(2)
	newStyle := &Style{Style: s.Style.MaxHeight(height)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleTabWidth(l *lua.LState) int {
	s := CheckStyle(l, 1)
	width := l.CheckInt(2)
	newStyle := &Style{Style: s.Style.TabWidth(width)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleCopy(l *lua.LState) int {
	s := CheckStyle(l, 1)
	newStyle := &Style{Style: s.Style}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleInherit(l *lua.LState) int {
	s := CheckStyle(l, 1)
	if ud, ok := l.Get(2).(*lua.LUserData); ok {
		if other, ok := ud.Value.(*Style); ok {
			newStyle := &Style{Style: s.Style.Inherit(other.Style)}
			ud := l.NewUserData()
			ud.Value = newStyle
			l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
			l.Push(ud)
			return 1
		}
	}
	l.ArgError(2, "Style expected")
	return 0
}

// Border styling methods
func styleBorderForeground(l *lua.LState) int {
	s := CheckStyle(l, 1)
	color := l.CheckString(2)
	newStyle := &Style{Style: s.Style.BorderForeground(lipgloss.Color(color))}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleBorderBackground(l *lua.LState) int {
	s := CheckStyle(l, 1)
	color := l.CheckString(2)
	newStyle := &Style{Style: s.Style.BorderBackground(lipgloss.Color(color))}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

// Individual border edge styling
func styleBorderTopForeground(l *lua.LState) int {
	s := CheckStyle(l, 1)
	color := l.CheckString(2)
	newStyle := &Style{Style: s.Style.BorderTopForeground(lipgloss.Color(color))}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleBorderBottomForeground(l *lua.LState) int {
	s := CheckStyle(l, 1)
	color := l.CheckString(2)
	newStyle := &Style{Style: s.Style.BorderBottomForeground(lipgloss.Color(color))}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleBorderLeftForeground(l *lua.LState) int {
	s := CheckStyle(l, 1)
	color := l.CheckString(2)
	newStyle := &Style{Style: s.Style.BorderLeftForeground(lipgloss.Color(color))}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleBorderRightForeground(l *lua.LState) int {
	s := CheckStyle(l, 1)
	color := l.CheckString(2)
	newStyle := &Style{Style: s.Style.BorderRightForeground(lipgloss.Color(color))}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleBorderTopBackground(l *lua.LState) int {
	s := CheckStyle(l, 1)
	color := l.CheckString(2)
	newStyle := &Style{Style: s.Style.BorderTopBackground(lipgloss.Color(color))}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleBorderBottomBackground(l *lua.LState) int {
	s := CheckStyle(l, 1)
	color := l.CheckString(2)
	newStyle := &Style{Style: s.Style.BorderBottomBackground(lipgloss.Color(color))}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleBorderLeftBackground(l *lua.LState) int {
	s := CheckStyle(l, 1)
	color := l.CheckString(2)
	newStyle := &Style{Style: s.Style.BorderLeftBackground(lipgloss.Color(color))}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleBorderRightBackground(l *lua.LState) int {
	s := CheckStyle(l, 1)
	color := l.CheckString(2)
	newStyle := &Style{Style: s.Style.BorderRightBackground(lipgloss.Color(color))}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

// Alignment methods
func styleAlignVertical(l *lua.LState) int {
	s := CheckStyle(l, 1)
	align := lipgloss.Position(l.CheckInt(2))
	newStyle := &Style{Style: s.Style.AlignVertical(align)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

// Space handling methods
func styleUnderlineSpaces(l *lua.LState) int {
	s := CheckStyle(l, 1)
	enabled := l.CheckBool(2)
	newStyle := &Style{Style: s.Style.UnderlineSpaces(enabled)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleStrikethroughSpaces(l *lua.LState) int {
	s := CheckStyle(l, 1)
	enabled := l.CheckBool(2)
	newStyle := &Style{Style: s.Style.StrikethroughSpaces(enabled)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

// Margin background
func styleMarginBackground(l *lua.LState) int {
	s := CheckStyle(l, 1)
	color := l.CheckString(2)
	newStyle := &Style{Style: s.Style.MarginBackground(lipgloss.Color(color))}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

// Text transformation
func styleSetString(l *lua.LState) int {
	s := CheckStyle(l, 1)
	str := l.CheckString(2)
	newStyle := &Style{Style: s.Style.SetString(str)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleGetValue(l *lua.LState) int {
	s := CheckStyle(l, 1)
	l.Push(lua.LString(s.Style.Value()))
	return 1
}
