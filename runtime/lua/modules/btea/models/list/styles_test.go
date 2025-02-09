package list

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/render"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
)

func TestLuaTableToStyles(t *testing.T) {
	tests := []struct {
		name           string
		setupLuaState  func(*lua.LState) *lua.LTable
		expectedStyles func() list.Styles
		expectError    bool
	}{
		{
			name: "successful conversion with all styles",
			setupLuaState: func(l *lua.LState) *lua.LTable {
				stylesTable := l.CreateTable(0, 16)

				// Create a sample style
				style := &render.Style{
					Style: lipgloss.NewStyle().
						Bold(true).
						Foreground(lipgloss.Color("#ff0000")),
				}

				// Create userdata for each style
				for _, key := range []string{
					"title_bar", "title", "spinner", "filter_prompt",
					"filter_cursor", "status_bar", "status_empty",
					"status_bar_active_filter", "status_bar_filter_count",
					"no_items", "pagination", "help",
					"active_pagination_dot", "inactive_pagination_dot",
					"arabic_pagination", "divider_dot",
				} {
					ud := l.NewUserData()
					ud.Value = style
					stylesTable.RawSetString(key, ud)
				}

				return stylesTable
			},
			expectedStyles: func() list.Styles {
				expectedStyle := lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("#ff0000"))

				styles := list.DefaultStyles()
				styles.TitleBar = expectedStyle
				styles.Title = expectedStyle
				styles.Spinner = expectedStyle
				styles.FilterPrompt = expectedStyle
				styles.FilterCursor = expectedStyle
				styles.StatusBar = expectedStyle
				styles.StatusEmpty = expectedStyle
				styles.StatusBarActiveFilter = expectedStyle
				styles.StatusBarFilterCount = expectedStyle
				styles.NoItems = expectedStyle
				styles.PaginationStyle = expectedStyle
				styles.HelpStyle = expectedStyle
				styles.ActivePaginationDot = expectedStyle
				styles.InactivePaginationDot = expectedStyle
				styles.ArabicPagination = expectedStyle
				styles.DividerDot = expectedStyle

				return styles
			},
			expectError: false,
		},
		{
			name: "partial styles table",
			setupLuaState: func(l *lua.LState) *lua.LTable {
				stylesTable := l.CreateTable(0, 2)

				style := &render.Style{
					Style: lipgloss.NewStyle().
						Bold(true).
						Foreground(lipgloss.Color("#ff0000")),
				}

				ud := l.NewUserData()
				ud.Value = style
				stylesTable.RawSetString("title", ud)

				return stylesTable
			},
			expectedStyles: func() list.Styles {
				styles := list.DefaultStyles()
				styles.Title = lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("#ff0000"))
				return styles
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			stylesTable := tt.setupLuaState(l)

			if tt.expectError {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("Expected an error but got none")
					}
				}()
			}

			result := luaTableToStyles(l, stylesTable)
			expected := tt.expectedStyles()

			// Compare specific fields
			assert.Equal(t, expected.Title, result.Title, "Title styles should match")
			assert.Equal(t, expected.TitleBar, result.TitleBar, "TitleBar styles should match")
			// Add more field comparisons as needed
		})
	}
}

func TestGetStyleFromUserData(t *testing.T) {
	tests := []struct {
		name        string
		setupValue  func(*lua.LState) lua.LValue
		expectStyle lipgloss.Style
		expectOk    bool
	}{
		{
			name: "valid style userdata",
			setupValue: func(l *lua.LState) lua.LValue {
				ud := l.NewUserData()
				ud.Value = &render.Style{
					Style: lipgloss.NewStyle().
						Bold(true).
						Foreground(lipgloss.Color("#ff0000")),
				}
				return ud
			},
			expectStyle: lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ff0000")),
			expectOk: true,
		},
		{
			name: "invalid userdata type",
			setupValue: func(l *lua.LState) lua.LValue {
				ud := l.NewUserData()
				ud.Value = "not a style"
				return ud
			},
			expectStyle: lipgloss.Style{},
			expectOk:    false,
		},
		{
			name: "non-userdata value",
			setupValue: func(l *lua.LState) lua.LValue {
				return lua.LString("not userdata")
			},
			expectStyle: lipgloss.Style{},
			expectOk:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			value := tt.setupValue(l)

			if !tt.expectOk {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("Expected a panic but got none")
					}
				}()
			}

			style, ok := getStyleFromUserData(l, value)

			if tt.expectOk {
				assert.True(t, ok, "Should return ok=true")
				assert.Equal(t, tt.expectStyle, style, "Styles should match")
			}
		})
	}
}

// TestIntegrationWithLuaVM tests the complete flow using actual Lua code
func TestIntegrationWithLuaVM(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	// Register any necessary functions/modules that your actual code uses
	// This is a simplified example - you might need to register more functions
	l.SetGlobal("create_style", l.NewFunction(func(l *lua.LState) int {
		style := &render.Style{
			Style: lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ff0000")),
		}
		ud := l.NewUserData()
		ud.Value = style
		l.Push(ud)
		return 1
	}))

	// Run Lua code that creates a styles table
	script := `
		local styles = {}
		styles.title = create_style()
		styles.title_bar = create_style()
		return styles
	`

	if err := l.DoString(script); err != nil {
		t.Fatalf("Failed to execute Lua script: %v", err)
	}

	// Create the returned styles table
	stylesTable := l.Get(-1).(*lua.LTable)
	l.Pop(1)

	// Convert to Go styles
	result := luaTableToStyles(l, stylesTable)

	// Verify the conversion
	expectedStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#ff0000"))

	assert.Equal(t, expectedStyle, result.Title, "Title style should match")
	assert.Equal(t, expectedStyle, result.TitleBar, "TitleBar style should match")
}
