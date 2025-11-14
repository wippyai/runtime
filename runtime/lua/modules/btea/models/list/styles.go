package list

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	"github.com/wippyai/runtime/runtime/lua/modules/btea/render"
	lua "github.com/yuin/gopher-lua"
)

// luaTableToStyles converts a Lua table to a list.Styles struct.
func luaTableToStyles(l *lua.LState, stylesTable *lua.LTable) list.Styles {
	listStyles := list.DefaultStyles()

	styleMap := map[string]*lipgloss.Style{
		"title_bar":                &listStyles.TitleBar,
		"title":                    &listStyles.Title,
		"spinner":                  &listStyles.Spinner,
		"filter_prompt":            &listStyles.FilterPrompt,
		"filter_cursor":            &listStyles.FilterCursor,
		"status_bar":               &listStyles.StatusBar,
		"status_empty":             &listStyles.StatusEmpty,
		"status_bar_active_filter": &listStyles.StatusBarActiveFilter,
		"status_bar_filter_count":  &listStyles.StatusBarFilterCount,
		"no_items":                 &listStyles.NoItems,
		"pagination":               &listStyles.PaginationStyle,
		"help":                     &listStyles.HelpStyle,
		"active_pagination_dot":    &listStyles.ActivePaginationDot,
		"inactive_pagination_dot":  &listStyles.InactivePaginationDot,
		"arabic_pagination":        &listStyles.ArabicPagination,
		"divider_dot":              &listStyles.DividerDot,
	}

	for styleName, stylePtr := range styleMap {
		if styleValue := stylesTable.RawGetString(styleName); styleValue.Type() == lua.LTUserData {
			if style, ok := getStyleFromUserData(l, styleValue); ok {
				*stylePtr = style
			}
		}
	}

	return listStyles
}

// getStyleFromUserData extracts a lipgloss.Style from a btea.Style userdata.
func getStyleFromUserData(l *lua.LState, styleUD lua.LValue) (lipgloss.Style, bool) {
	ud, ok := styleUD.(*lua.LUserData)
	if !ok {
		l.RaiseError("Expected a btea.Style userdata")
		return lipgloss.Style{}, false
	}

	style, ok := ud.Value.(*render.Style)
	if !ok {
		l.RaiseError("Expected a btea.Style userdata")
		return lipgloss.Style{}, false
	}

	return style.Style, true
}
