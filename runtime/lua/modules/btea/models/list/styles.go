package list

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/render"
	lua "github.com/yuin/gopher-lua"
)

// luaTableToStyles converts a Lua table to a list.Styles struct.
func luaTableToStyles(l *lua.LState, styles *lua.LTable) list.Styles {
	listStyles := list.DefaultStyles()

	if titleBar := styles.RawGetString("title_bar"); titleBar.Type() == lua.LTUserData {
		if style, ok := getStyleFromUserData(l, titleBar); ok {
			listStyles.TitleBar = style
		}
	}
	if title := styles.RawGetString("title"); title.Type() == lua.LTUserData {
		if style, ok := getStyleFromUserData(l, title); ok {
			listStyles.Title = style
		}
	}
	if spinner := styles.RawGetString("spinner"); spinner.Type() == lua.LTUserData {
		if style, ok := getStyleFromUserData(l, spinner); ok {
			listStyles.Spinner = style
		}
	}
	if filterPrompt := styles.RawGetString("filter_prompt"); filterPrompt.Type() == lua.LTUserData {
		if style, ok := getStyleFromUserData(l, filterPrompt); ok {
			listStyles.FilterPrompt = style
		}
	}
	if filterCursor := styles.RawGetString("filter_cursor"); filterCursor.Type() == lua.LTUserData {
		if style, ok := getStyleFromUserData(l, filterCursor); ok {
			listStyles.FilterCursor = style
		}
	}
	if statusBar := styles.RawGetString("status_bar"); statusBar.Type() == lua.LTUserData {
		if style, ok := getStyleFromUserData(l, statusBar); ok {
			listStyles.StatusBar = style
		}
	}
	if statusEmpty := styles.RawGetString("status_empty"); statusEmpty.Type() == lua.LTUserData {
		if style, ok := getStyleFromUserData(l, statusEmpty); ok {
			listStyles.StatusEmpty = style
		}
	}
	if statusBarActiveFilter := styles.RawGetString("status_bar_active_filter"); statusBarActiveFilter.Type() == lua.LTUserData {
		if style, ok := getStyleFromUserData(l, statusBarActiveFilter); ok {
			listStyles.StatusBarActiveFilter = style
		}
	}
	if statusBarFilterCount := styles.RawGetString("status_bar_filter_count"); statusBarFilterCount.Type() == lua.LTUserData {
		if style, ok := getStyleFromUserData(l, statusBarFilterCount); ok {
			listStyles.StatusBarFilterCount = style
		}
	}
	if noItems := styles.RawGetString("no_items"); noItems.Type() == lua.LTUserData {
		if style, ok := getStyleFromUserData(l, noItems); ok {
			listStyles.NoItems = style
		}
	}
	if pagination := styles.RawGetString("pagination"); pagination.Type() == lua.LTUserData {
		if style, ok := getStyleFromUserData(l, pagination); ok {
			listStyles.PaginationStyle = style
		}
	}
	if help := styles.RawGetString("help"); help.Type() == lua.LTUserData {
		if style, ok := getStyleFromUserData(l, help); ok {
			listStyles.HelpStyle = style
		}
	}
	if activePaginationDot := styles.RawGetString("active_pagination_dot"); activePaginationDot.Type() == lua.LTUserData {
		if style, ok := getStyleFromUserData(l, activePaginationDot); ok {
			listStyles.ActivePaginationDot = style
		}
	}
	if inactivePaginationDot := styles.RawGetString("inactive_pagination_dot"); inactivePaginationDot.Type() == lua.LTUserData {
		if style, ok := getStyleFromUserData(l, inactivePaginationDot); ok {
			listStyles.InactivePaginationDot = style
		}
	}
	if arabicPagination := styles.RawGetString("arabic_pagination"); arabicPagination.Type() == lua.LTUserData {
		if style, ok := getStyleFromUserData(l, arabicPagination); ok {
			listStyles.ArabicPagination = style
		}
	}
	if dividerDot := styles.RawGetString("divider_dot"); dividerDot.Type() == lua.LTUserData {
		if style, ok := getStyleFromUserData(l, dividerDot); ok {
			listStyles.DividerDot = style
		}
	}

	return listStyles
}

// getStyleFromUserData extracts a lipgloss.Style from a btea.Style userdata.
func getStyleFromUserData(l *lua.LState, styleUD lua.LValue) (lipgloss.Style, bool) {
	if ud, ok := styleUD.(*lua.LUserData); ok {
		if style, ok := ud.Value.(*render.Style); ok {
			return style.Style, true
		}
	}
	l.RaiseError("Expected a btea.Style userdata")
	return lipgloss.Style{}, false
}
