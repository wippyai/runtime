package render

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

func setupTextState(_ *testing.T) *lua.LState {
	L := lua.NewState()
	mod := L.NewTable()
	RegisterTextUtils(L, mod)
	L.SetGlobal("btea", mod)
	return L
}

func TestTextMeasurementFunctions(t *testing.T) {
	L := setupTextState(t)
	defer L.Close()

	script := `
		local text = btea.text
		
		-- Test width
		local w = text.width("test")
		assert(w == 4, "width should be 4")
		
		-- Test height
		local h = text.height("test\ntext")
		assert(h == 2, "height should be 2")
		
		-- Test size
		local width, height = text.size("test\nlong text")
		assert(width == 9, "width should be 9")
		assert(height == 2, "height should be 2")
		
		-- Test max_width
		local strings = {"test", "longer text", "short"}
		local max_w = text.max_width(strings)
		assert(max_w == 11, "max width should be 11")
		
		-- Test max_height
		local multiline = {"test", "test\ntest", "test\ntest\ntest"}
		local max_h = text.max_height(multiline)
		assert(max_h == 3, "max height should be 3")
	`
	require.NoError(t, L.DoString(script))
}

func TestTextJoinFunctions(t *testing.T) {
	L := setupTextState(t)
	defer L.Close()

	script := `
		local text = btea.text
		
		-- Test horizontal join with different positions
		local left_join = text.join_horizontal(text.position.LEFT, "A", "B")
		assert(left_join == "AB", "left join should concatenate strings")
		
		local center_join = text.join_horizontal(text.position.CENTER,
			"A\nA",
			"B\nB\nB"
		)
		assert(text.height(center_join) == 3, "center join should preserve height")
		
		-- Test vertical join with different positions
		local top_join = text.join_vertical(text.position.TOP, "A", "B")
		assert(top_join == "A\nB", "top join should stack strings")
		
		local bottom_join = text.join_vertical(text.position.BOTTOM,
			"AAA",
			"B"
		)
		assert(text.width(bottom_join) == 3, "bottom join should preserve width")
	`
	require.NoError(t, L.DoString(script))
}

func TestTextStyleRunes(t *testing.T) {
	L := setupTextState(t)
	defer L.Close()

	// Register Style for testing
	mod := L.GetGlobal("btea").(*lua.LTable)
	RegisterStyle(L, mod)

	script := `
		local text = btea.text
		
		-- Spawn test styles
		local matched_style = btea.style():bold()
		local unmatched_style = btea.style()
		
		-- Test styling specific runes
		local result = text.style_runes(
			"test",
			{1, 4}, -- style first and last character
			matched_style,
			unmatched_style
		)
		
		-- Check length is different due to ANSI codes
		assert(#result > #"test", "styled string should be longer due to ANSI codes")
	`
	require.NoError(t, L.DoString(script))
}

func TestTextSanitizeRunes(t *testing.T) {
	L := setupTextState(t)
	defer L.Close()

	script := `
		local text = btea.text
		
		-- Test default replacements
		local input = "test\ntest\ttest"
		local sanitized = text.sanitize_runes(input)
		local expected = "test\ntest    test"
		assert(sanitized == expected, "should replace tab with spaces")
		
		-- Test custom replacements
		local custom = text.sanitize_runes(
			"test\ntest\ttest",
			"<br>",    -- newline replacement
			"<tab>"    -- tab replacement
		)
		assert(custom == "test<br>test<tab>test", "should use custom replacements")
		
		-- Test with mixed input
		local mixed = text.sanitize_runes("hello\tworld\n!")
		assert(mixed == "hello    world\n!", "should handle mixed input correctly")
	`
	require.NoError(t, L.DoString(script))
}

func TestTextPositionConstants(t *testing.T) {
	L := setupTextState(t)
	defer L.Close()

	script := `
		local text = btea.text
		
		-- Test position constants
		assert(text.position.TOP == 0.0, "TOP should be 0.0")
		assert(text.position.CENTER == 0.5, "CENTER should be 0.5")
		assert(text.position.BOTTOM == 1.0, "BOTTOM should be 1.0")
		assert(text.position.LEFT == 0.0, "LEFT should be 0.0")
		assert(text.position.RIGHT == 1.0, "RIGHT should be 1.0")
		
		-- Test constants in join functions
		local h_join = text.join_horizontal(text.position.LEFT, "A", "B")
		local v_join = text.join_vertical(text.position.TOP, "A", "B")
		
		assert(h_join == "AB", "horizontal join with LEFT should work")
		assert(v_join == "A\nB", "vertical join with TOP should work")
	`
	require.NoError(t, L.DoString(script))
}

func TestTextErrorHandling(t *testing.T) {
	L := setupTextState(t)
	defer L.Close()

	tests := []struct {
		name   string
		script string
	}{
		{
			name: "invalid max_width input",
			script: `
				local text = btea.text
				local w = text.max_width("not a table")
			`,
		},
		{
			name: "invalid style_runes style parameter",
			script: `
				local text = btea.text
				local result = text.style_runes("test", {1}, "not a style", "not a style")
			`,
		},
		{
			name: "nil table in max_height",
			script: `
				local text = btea.text
				local h = text.max_height(nil)
			`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := L.DoString(tt.script)
			require.Error(t, err, "should handle invalid input")
		})
	}
}
