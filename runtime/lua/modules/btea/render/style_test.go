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
