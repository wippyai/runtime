package render

import (
	"github.com/muesli/termenv"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/require"
	"github.com/yuin/gopher-lua"
)

// setupState initializes a new Lua state with the btea module registered.
// It also sets the lipgloss color profile to TrueColor to ensure ANSI escape codes
// are applied (e.g., for bold styling).
func setupState(t *testing.T) *lua.LState {
	// Force ANSI color output.
	lipgloss.SetColorProfile(termenv.ANSI)

	L := lua.NewState()
	mod := L.NewTable()
	RegisterStyle(L, mod)
	L.SetGlobal("btea", mod)
	return L
}

func TestStyleBasicRender(t *testing.T) {
	L := setupState(t)
	defer L.Close()

	script := `
		local s = btea.new_style()
		local output = s:render("hello")
		assert(type(output) == "string", "render should return a string")
	`
	require.NoError(t, L.DoString(script))
}

func TestStyleColors(t *testing.T) {
	L := setupState(t)
	defer L.Close()

	script := `
		local s = btea.new_style()
		local s_fg = s:foreground("red")
		local s_bg = s:background("blue")
		assert(type(s_fg) == "userdata", "foreground should return a Style")
		assert(type(s_bg) == "userdata", "background should return a Style")
	`
	require.NoError(t, L.DoString(script))
}

func TestStyleTextModifiers(t *testing.T) {
	L := setupState(t)
	defer L.Close()

	script := `
		local s = btea.new_style()
		local bold   = s:bold()
		local italic = s:italic()
		local underline = s:underline()
		local strike = s:strikethrough()
		local faint  = s:faint()
		local blink  = s:blink()
		local reverse = s:reverse()

		assert(type(bold) == "userdata", "bold should return a Style")
		assert(type(italic) == "userdata", "italic should return a Style")
		assert(type(underline) == "userdata", "underline should return a Style")
		assert(type(strike) == "userdata", "strikethrough should return a Style")
		assert(type(faint) == "userdata", "faint should return a Style")
		assert(type(blink) == "userdata", "blink should return a Style")
		assert(type(reverse) == "userdata", "reverse should return a Style")
		
		-- Verify that modifiers change the output.
		local plain = s:render("test")
		local bolded = bold:render("test")
		-- When the color profile is set to TrueColor, bold should wrap the text with ANSI codes.
		assert(plain ~= bolded, "Bold Style should alter the render output")
	`
	require.NoError(t, L.DoString(script))
}

func TestStyleLayout(t *testing.T) {
	L := setupState(t)
	defer L.Close()

	script := `
		local s = btea.new_style()
		local padded = s:padding(1, 2, 3, 4)
		local margined = s:margin(1, 2, 3, 4)
		assert(type(padded) == "userdata", "padding should return a Style")
		assert(type(margined) == "userdata", "margin should return a Style")
	`
	require.NoError(t, L.DoString(script))
}

func TestStyleBorders(t *testing.T) {
	L := setupState(t)
	defer L.Close()

	script := `
		local s = btea.new_style()
		local rounded = s:border("rounded")
		local custom = s:custom_border({ top = "-", bottom = "-", left = "|", right = "|" })
		assert(type(rounded) == "userdata", "border should return a Style")
		assert(type(custom) == "userdata", "custom_border should return a Style")
	`
	require.NoError(t, L.DoString(script))
}

func TestStyleDimensions(t *testing.T) {
	L := setupState(t)
	defer L.Close()

	script := `
		local s = btea.new_style()
		local widthed = s:width(50)
		local heighted = s:height(10)
		local maxWidthed = s:max_width(60)
		local maxHeighted = s:max_height(15)
		local tabW = s:tab_width(4)
		assert(type(widthed) == "userdata", "width should return a Style")
		assert(type(heighted) == "userdata", "height should return a Style")
		assert(type(maxWidthed) == "userdata", "max_width should return a Style")
		assert(type(maxHeighted) == "userdata", "max_height should return a Style")
		assert(type(tabW) == "userdata", "tab_width should return a Style")
	`
	require.NoError(t, L.DoString(script))
}

func TestStyleAlignmentAndInline(t *testing.T) {
	L := setupState(t)
	defer L.Close()

	script := `
		local s = btea.new_style()
		local aligned = s:align(btea.align.CENTER)
		local inlined = s:inline(true)
		assert(type(aligned) == "userdata", "align should return a Style")
		assert(type(inlined) == "userdata", "inline should return a Style")
	`
	require.NoError(t, L.DoString(script))
}

func TestStyleCopyAndInherit(t *testing.T) {
	L := setupState(t)
	defer L.Close()

	script := `
		local s = btea.new_style()
		local s_copy = s:copy()
		assert(type(s_copy) == "userdata", "copy should return a Style")
		
		-- Verify that a copy renders the same text.
		local orig = s:render("copy test")
		local copy_render = s_copy:render("copy test")
		assert(orig == copy_render, "copied Style should render the same output")
		
		-- Test inherit: inheriting a bold Style should change output.
		local s_bold = s:bold()
		local inherited = s:inherit(s_bold)
		local inherited_render = inherited:render("inherit test")
		assert(orig ~= inherited_render, "inherited Style should alter the render output")
	`
	require.NoError(t, L.DoString(script))
}

func TestStyleBorderColors(t *testing.T) {
	L := setupState(t)
	defer L.Close()

	script := `
		local s = btea.new_style()
		local bf = s:border_foreground("red")
		local bb = s:border_background("blue")
		
		-- Test individual border edge colors
		local btf = s:border_top_foreground("green")
		local bbf = s:border_bottom_foreground("yellow")
		local blf = s:border_left_foreground("cyan")
		local brf = s:border_right_foreground("magenta")
		
		local btb = s:border_top_background("black")
		local bbb = s:border_bottom_background("white")
		local blb = s:border_left_background("gray")
		local brb = s:border_right_background("orange")

		assert(type(bf) == "userdata", "border_foreground should return a Style")
		assert(type(bb) == "userdata", "border_background should return a Style")
		assert(type(btf) == "userdata", "border_top_foreground should return a Style")
		assert(type(bbf) == "userdata", "border_bottom_foreground should return a Style")
		assert(type(blf) == "userdata", "border_left_foreground should return a Style")
		assert(type(brf) == "userdata", "border_right_foreground should return a Style")
		assert(type(btb) == "userdata", "border_top_background should return a Style")
		assert(type(bbb) == "userdata", "border_bottom_background should return a Style")
		assert(type(blb) == "userdata", "border_left_background should return a Style")
		assert(type(brb) == "userdata", "border_right_background should return a Style")
	`
	require.NoError(t, L.DoString(script))
}

func TestStyleVerticalAlignment(t *testing.T) {
	L := setupState(t)
	defer L.Close()

	script := `
		local s = btea.new_style()
		local v_aligned = s:align_vertical(btea.align.CENTER)
		assert(type(v_aligned) == "userdata", "align_vertical should return a Style")
		
		-- Test different alignment positions
		local top = s:align_vertical(btea.align.LEFT)      -- LEFT is used as TOP in vertical alignment
		local bottom = s:align_vertical(btea.align.RIGHT)  -- RIGHT is used as BOTTOM in vertical alignment
		assert(type(top) == "userdata", "top alignment should return a Style")
		assert(type(bottom) == "userdata", "bottom alignment should return a Style")
	`
	require.NoError(t, L.DoString(script))
}

func TestStyleSpaceHandling(t *testing.T) {
	L := setupState(t)
	defer L.Close()

	script := `
		local s = btea.new_style()
		local underline_spaces = s:underline_spaces(true)
		local strikethrough_spaces = s:strikethrough_spaces(true)
		local color_whitespace = s:color_whitespace(true)
		
		assert(type(underline_spaces) == "userdata", "underline_spaces should return a Style")
		assert(type(strikethrough_spaces) == "userdata", "strikethrough_spaces should return a Style")
		assert(type(color_whitespace) == "userdata", "color_whitespace should return a Style")
		
		-- Test output differences
		local base = s:render("  test  ")
		local with_underline = underline_spaces:render("  test  ")
		local with_strike = strikethrough_spaces:render("  test  ")
		assert(base ~= with_underline, "underline_spaces should modify the output")
		assert(base ~= with_strike, "strikethrough_spaces should modify the output")
	`
	require.NoError(t, L.DoString(script))
}

func TestStyleMarginBackground(t *testing.T) {
	L := setupState(t)
	defer L.Close()

	script := `
		local s = btea.new_style()
		local with_margin_bg = s:margin_background("red")
		assert(type(with_margin_bg) == "userdata", "margin_background should return a Style")
		
		-- Test with different combinations
		local with_margin = s:margin(1):margin_background("blue")
		assert(type(with_margin) == "userdata", "margin with background should return a Style")
	`
	require.NoError(t, L.DoString(script))
}
