package btea

import (
	"github.com/charmbracelet/lipgloss"
	lua "github.com/yuin/gopher-lua"
)

// Style wraps lipgloss.Style for Lua
type Style struct {
	style lipgloss.Style
}

// RegisterStyle registers the style component
func RegisterStyle(l *lua.LState, mod *lua.LTable) {
	// Create and register the style metatable
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
	// Create new style
	ud := l.NewUserData()
	ud.Value = &Style{style: lipgloss.NewStyle()}
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func checkStyle(l *lua.LState) *Style {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Style); ok {
		return v
	}
	l.ArgError(1, "style expected")
	return nil
}

// Style methods

func styleRender(l *lua.LState) int {
	s := checkStyle(l)
	str := l.CheckString(2)
	l.Push(lua.LString(s.style.Render(str)))
	return 1
}

func styleForeground(l *lua.LState) int {
	s := checkStyle(l)
	color := l.CheckString(2)
	newStyle := &Style{style: s.style.Copy().Foreground(lipgloss.Color(color))}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleBackground(l *lua.LState) int {
	s := checkStyle(l)
	color := l.CheckString(2)
	newStyle := &Style{style: s.style.Copy().Background(lipgloss.Color(color))}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleBold(l *lua.LState) int {
	s := checkStyle(l)
	newStyle := &Style{style: s.style.Copy().Bold(true)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleItalic(l *lua.LState) int {
	s := checkStyle(l)
	newStyle := &Style{style: s.style.Copy().Italic(true)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleUnderline(l *lua.LState) int {
	s := checkStyle(l)
	newStyle := &Style{style: s.style.Copy().Underline(true)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleStrikethrough(l *lua.LState) int {
	s := checkStyle(l)
	newStyle := &Style{style: s.style.Copy().Strikethrough(true)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleFaint(l *lua.LState) int {
	s := checkStyle(l)
	newStyle := &Style{style: s.style.Copy().Faint(true)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleBlink(l *lua.LState) int {
	s := checkStyle(l)
	newStyle := &Style{style: s.style.Copy().Blink(true)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleReverse(l *lua.LState) int {
	s := checkStyle(l)
	newStyle := &Style{style: s.style.Copy().Reverse(true)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func stylePadding(l *lua.LState) int {
	s := checkStyle(l)
	top := l.CheckInt(2)
	right := l.OptInt(3, top)
	bottom := l.OptInt(4, top)
	left := l.OptInt(5, right)
	newStyle := &Style{style: s.style.Copy().Padding(top, right, bottom, left)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleMargin(l *lua.LState) int {
	s := checkStyle(l)
	top := l.CheckInt(2)
	right := l.OptInt(3, top)
	bottom := l.OptInt(4, top)
	left := l.OptInt(5, right)
	newStyle := &Style{style: s.style.Copy().Margin(top, right, bottom, left)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleBorder(l *lua.LState) int {
	s := checkStyle(l)
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
	newStyle := &Style{style: s.style.Copy().Border(border)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

// styleCustomBorder allows the user to specify a custom border via a Lua table.
// Expected keys (all optional) are: top, bottom, left, right, topleft, topright, bottomleft, bottomright.
func styleCustomBorder(l *lua.LState) int {
	s := checkStyle(l)
	borderTbl := l.CheckTable(2)

	var b lipgloss.Border

	// Optional keys: "top", "bottom", "left", "right", "topleft", "topright", "bottomleft", "bottomright"
	borderTbl.ForEach(func(key, value lua.LValue) {
		keyStr := lua.LVAsString(key)
		valStr := lua.LVAsString(value)
		switch keyStr {
		case "top":
			b.Top = valStr
		case "bottom":
			b.Bottom = valStr
		case "left":
			b.Left = valStr
		case "right":
			b.Right = valStr
		case "top_left":
			b.TopLeft = valStr
		case "top_right":
			b.TopRight = valStr
		case "bottom_left":
			b.BottomLeft = valStr
		case "bottom_right":
			b.BottomRight = valStr
		}
	})

	newStyle := &Style{style: s.style.Copy().Border(b)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleWidth(l *lua.LState) int {
	s := checkStyle(l)
	width := l.CheckInt(2)
	newStyle := &Style{style: s.style.Copy().Width(width)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleHeight(l *lua.LState) int {
	s := checkStyle(l)
	height := l.CheckInt(2)
	newStyle := &Style{style: s.style.Copy().Height(height)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleAlign(l *lua.LState) int {
	s := checkStyle(l)
	align := lipgloss.Position(l.CheckInt(2))
	newStyle := &Style{style: s.style.Copy().Align(align)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleInline(l *lua.LState) int {
	s := checkStyle(l)
	inline := l.CheckBool(2)
	newStyle := &Style{style: s.style.Copy().Inline(inline)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleMaxWidth(l *lua.LState) int {
	s := checkStyle(l)
	width := l.CheckInt(2)
	newStyle := &Style{style: s.style.Copy().MaxWidth(width)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleMaxHeight(l *lua.LState) int {
	s := checkStyle(l)
	height := l.CheckInt(2)
	newStyle := &Style{style: s.style.Copy().MaxHeight(height)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleTabWidth(l *lua.LState) int {
	s := checkStyle(l)
	tabWidth := l.CheckInt(2)
	newStyle := &Style{style: s.style.Copy().TabWidth(tabWidth)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleCopy(l *lua.LState) int {
	s := checkStyle(l)
	newStyle := &Style{style: s.style.Copy()}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}

func styleInherit(l *lua.LState) int {
	s := checkStyle(l)
	other := checkStyle(l)
	newStyle := &Style{style: s.style.Copy().Inherit(other.style)}
	ud := l.NewUserData()
	ud.Value = newStyle
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Style"))
	l.Push(ud)
	return 1
}
