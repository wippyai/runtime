package models

import (
	"github.com/charmbracelet/bubbles/paginator"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	lua "github.com/yuin/gopher-lua"
)

// Paginator wraps paginator.Model for Lua
type Paginator struct {
	model paginator.Model
}

func (p *Paginator) Init() tea.Cmd {
	return nil
}

func (p *Paginator) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	p.model, cmd = p.model.Update(msg)
	return p, cmd
}

func (p *Paginator) View() string {
	return p.View()
}

// RegisterPaginator registers the paginator component
func RegisterPaginator(l *lua.LState, mod *lua.LTable) {
	// Create and register the paginator metatable
	mt := l.NewTypeMetatable("btea.Paginator")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		// Core methods
		"update":           paginatorUpdate,
		"view":             paginatorView,
		"set_total_pages":  paginatorSetTotalPages,
		"prev_page":        paginatorPrevPage,
		"next_page":        paginatorNextPage,
		"items_on_page":    paginatorItemsOnPage,
		"get_slice_bounds": paginatorGetSliceBounds,
		"on_first_page":    paginatorOnFirstPage,
		"on_last_page":     paginatorOnLastPage,
		"get_current_page": paginatorGetCurrentPage,
		"set_per_page":     paginatorSetPerPage,
		"get_type":         paginatorGetType,
		"set_type":         paginatorSetType,
	}))

	// Register type constants
	typesTbl := l.NewTable()
	l.SetField(typesTbl, "ARABIC", lua.LNumber(paginator.Arabic))
	l.SetField(typesTbl, "DOTS", lua.LNumber(paginator.Dots))
	l.SetField(mod, "paginator_types", typesTbl)

	// Register constructor
	l.SetField(mod, "paginator", l.NewFunction(newPaginator))
}

func newPaginator(l *lua.LState) int {
	opts := l.CheckTable(1)

	p := &Paginator{
		model: paginator.New(),
	}

	// Process options
	opts.ForEach(func(k, v lua.LValue) {
		switch k.String() {
		case "type":
			if n, ok := v.(lua.LNumber); ok {
				p.model.Type = paginator.Type(int(n))
			}
		case "page":
			if n, ok := v.(lua.LNumber); ok {
				p.model.Page = int(n)
			}
		case "per_page":
			if n, ok := v.(lua.LNumber); ok {
				p.model.PerPage = int(n)
			}
		case "total_pages":
			if n, ok := v.(lua.LNumber); ok {
				p.model.TotalPages = int(n)
			}
		case "active_dot":
			if s, ok := v.(lua.LString); ok {
				p.model.ActiveDot = string(s)
			}
		case "inactive_dot":
			if s, ok := v.(lua.LString); ok {
				p.model.InactiveDot = string(s)
			}
		case "arabic_format":
			if s, ok := v.(lua.LString); ok {
				p.model.ArabicFormat = string(s)
			}
		}
	})

	// Create userdata
	ud := l.NewUserData()
	ud.Value = p
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Paginator"))
	l.Push(ud)
	return 1
}

func checkPaginator(l *lua.LState) *Paginator {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Paginator); ok {
		return v
	}
	l.ArgError(1, "paginator expected")
	return nil
}

// Paginator methods

func paginatorUpdate(l *lua.LState) int {
	p := checkPaginator(l)
	if p == nil {
		return 0
	}

	msgValue := l.CheckAny(2)
	msg, err := protocol.LuaToMsg(msgValue)
	if err != nil {
		l.RaiseError("failed to convert message: %v", err)
		return 0
	}

	var cmd tea.Cmd
	p.model, cmd = p.model.Update(msg)

	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
	}
	return 0
}

func paginatorView(l *lua.LState) int {
	p := checkPaginator(l)
	if p == nil {
		return 0
	}
	l.Push(lua.LString(p.model.View()))
	return 1
}

func paginatorSetTotalPages(l *lua.LState) int {
	p := checkPaginator(l)
	if p == nil {
		return 0
	}
	items := l.CheckInt(2)
	totalPages := p.model.SetTotalPages(items)
	l.Push(lua.LNumber(totalPages))
	return 1
}

func paginatorPrevPage(l *lua.LState) int {
	p := checkPaginator(l)
	if p == nil {
		return 0
	}
	p.model.PrevPage()
	return 0
}

func paginatorNextPage(l *lua.LState) int {
	p := checkPaginator(l)
	if p == nil {
		return 0
	}
	p.model.NextPage()
	return 0
}

func paginatorItemsOnPage(l *lua.LState) int {
	p := checkPaginator(l)
	if p == nil {
		return 0
	}
	totalItems := l.CheckInt(2)
	items := p.model.ItemsOnPage(totalItems)
	l.Push(lua.LNumber(items))
	return 1
}

func paginatorGetSliceBounds(l *lua.LState) int {
	p := checkPaginator(l)
	if p == nil {
		return 0
	}
	length := l.CheckInt(2)
	start, end := p.model.GetSliceBounds(length)
	l.Push(lua.LNumber(start))
	l.Push(lua.LNumber(end))
	return 2
}

func paginatorOnFirstPage(l *lua.LState) int {
	p := checkPaginator(l)
	if p == nil {
		return 0
	}
	l.Push(lua.LBool(p.model.OnFirstPage()))
	return 1
}

func paginatorOnLastPage(l *lua.LState) int {
	p := checkPaginator(l)
	if p == nil {
		return 0
	}
	l.Push(lua.LBool(p.model.OnLastPage()))
	return 1
}

func paginatorGetCurrentPage(l *lua.LState) int {
	p := checkPaginator(l)
	if p == nil {
		return 0
	}
	l.Push(lua.LNumber(p.model.Page))
	return 1
}

func paginatorSetPerPage(l *lua.LState) int {
	p := checkPaginator(l)
	if p == nil {
		return 0
	}
	perPage := l.CheckInt(2)
	p.model.PerPage = perPage
	return 0
}

func paginatorGetType(l *lua.LState) int {
	p := checkPaginator(l)
	if p == nil {
		return 0
	}
	l.Push(lua.LNumber(p.model.Type))
	return 1
}

func paginatorSetType(l *lua.LState) int {
	p := checkPaginator(l)
	if p == nil {
		return 0
	}
	pType := paginator.Type(l.CheckNumber(2))
	p.model.Type = pType
	return 0
}
