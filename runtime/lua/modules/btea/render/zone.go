package render

import (
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	lua "github.com/yuin/gopher-lua"
)

// Manager wraps zone.Manager for Lua
type Manager struct {
	model *zone.Manager
}

// Model returns the underlying zone.Manager
func (m *Manager) Model() *zone.Manager {
	return m.model
}

// ZoneInfo wraps zone.ZoneInfo for Lua
type ZoneInfo struct {
	model *zone.ZoneInfo
}

// Model returns the underlying zone.ZoneInfo
func (z *ZoneInfo) Model() *zone.ZoneInfo {
	return z.model
}

// RegisterZone registers the zone component
func RegisterZone(l *lua.LState, mod *lua.LTable) {
	// Register Manager type
	mt := l.NewTypeMetatable("btea.ZoneManager")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"mark":                 managerMark,
		"get":                  managerGet,
		"scan":                 managerScan,
		"new_prefix":           managerNewPrefix,
		"set_enabled":          managerSetEnabled,
		"is_enabled":           managerIsEnabled,
		"any_in_bounds":        managerAnyInBounds,
		"any_in_bounds_update": managerAnyInBoundsAndUpdate,
	}))

	// Register ZoneInfo type
	zimt := l.NewTypeMetatable("btea.ZoneInfo")
	l.SetField(zimt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"in_bounds": zoneInfoInBounds,
		"pos":       zoneInfoPos,
		"is_zero":   zoneInfoIsZero,
	}))

	// Register constructor in the module
	l.SetField(mod, "new_zone_manager", l.NewFunction(newManager))
}

// Manager methods

func newManager(l *lua.LState) int {
	m := &Manager{
		model: zone.New(),
	}
	ud := l.NewUserData()
	ud.Value = m
	l.SetMetatable(ud, l.GetTypeMetatable("btea.ZoneManager"))
	l.Push(ud)
	return 1
}

func checkManager(l *lua.LState) *Manager {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Manager); ok {
		return v
	}
	l.ArgError(1, "zone manager expected")
	return nil
}

func managerMark(l *lua.LState) int {
	m := checkManager(l)
	id := l.CheckString(2)
	content := l.CheckString(3)
	l.Push(lua.LString(m.model.Mark(id, content)))
	return 1
}

func managerGet(l *lua.LState) int {
	m := checkManager(l)
	id := l.CheckString(2)
	if info := m.model.Get(id); info != nil {
		ud := l.NewUserData()
		ud.Value = &ZoneInfo{model: info}
		l.SetMetatable(ud, l.GetTypeMetatable("btea.ZoneInfo"))
		l.Push(ud)
	} else {
		l.Push(lua.LNil)
	}
	return 1
}

func managerScan(l *lua.LState) int {
	m := checkManager(l)
	content := l.CheckString(2)
	l.Push(lua.LString(m.model.Scan(content)))
	return 1
}

func managerNewPrefix(l *lua.LState) int {
	m := checkManager(l)
	l.Push(lua.LString(m.model.NewPrefix()))
	return 1
}

func managerSetEnabled(l *lua.LState) int {
	m := checkManager(l)
	enabled := l.CheckBool(2)
	m.model.SetEnabled(enabled)
	return 0
}

func managerIsEnabled(l *lua.LState) int {
	m := checkManager(l)
	l.Push(lua.LBool(m.model.Enabled()))
	return 1
}

func managerAnyInBounds(l *lua.LState) int {
	m := checkManager(l)

	model := l.CheckUserData(2)
	tModel, ok := model.Value.(tea.Model)
	if !ok {
		l.ArgError(2, "model expected")
		return 0
	}

	msgValue := l.CheckAny(3)
	msg, err := protocol.LuaToMsg(msgValue)
	if err != nil {
		l.RaiseError("failed to convert message: %v", err)
		return 0
	}
	mouseMsg, ok := msg.(tea.MouseMsg)
	if !ok {
		l.ArgError(3, "mouse message expected")
		return 0
	}
	m.model.AnyInBounds(tModel, mouseMsg)
	return 0
}

func managerAnyInBoundsAndUpdate(l *lua.LState) int {
	m := checkManager(l)
	if m == nil {
		return 0
	}

	modelValue := l.CheckAny(2)
	model, ok := protocol.TryGetModel(modelValue)
	if !ok {
		l.ArgError(2, "model expected")
		return 0
	}

	// Convert message
	msgValue := l.CheckAny(3)
	msg, err := protocol.LuaToMsg(msgValue)
	if err != nil {
		l.RaiseError("failed to convert message: %v", err)
		return 0
	}

	// Update the model
	newModel, cmd := m.model.AnyInBoundsAndUpdate(model, msg.(tea.MouseMsg))
	protocol.UpdateModelValue(modelValue, newModel)

	// Return just the command if there is one
	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
	}
	return 0
}

// ZoneInfo methods

func checkZoneInfo(l *lua.LState) *ZoneInfo {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*ZoneInfo); ok {
		return v
	}
	l.ArgError(1, "zone info expected")
	return nil
}

func zoneInfoInBounds(l *lua.LState) int {
	z := checkZoneInfo(l)
	msgValue := l.CheckAny(2)
	msg, err := protocol.LuaToMsg(msgValue)

	if err != nil {
		l.RaiseError("failed to convert message: %v", err)
		return 0
	}
	mouseMsg, ok := msg.(tea.MouseMsg)
	if !ok {
		l.ArgError(2, "mouse message expected")
		return 0
	}
	l.Push(lua.LBool(z.model.InBounds(mouseMsg)))
	return 1
}

func zoneInfoPos(l *lua.LState) int {
	z := checkZoneInfo(l)
	msgValue := l.CheckAny(2)
	msg, err := protocol.LuaToMsg(msgValue)
	if err != nil {
		l.RaiseError("failed to convert message: %v", err)
		return 0
	}
	mouseMsg, ok := msg.(tea.MouseMsg)
	if !ok {
		l.ArgError(2, "mouse message expected")
		return 0
	}
	x, y := z.model.Pos(mouseMsg)
	l.Push(lua.LNumber(x))
	l.Push(lua.LNumber(y))
	return 2
}

func zoneInfoIsZero(l *lua.LState) int {
	z := checkZoneInfo(l)
	l.Push(lua.LBool(z.model.IsZero()))
	return 1
}
