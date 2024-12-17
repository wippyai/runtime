package httphandler

import "github.com/ponyruntime/go-lua"

func (m *Module) new(l *lua.LState) int {
	m.log.Debug("called http handler 'new' function")
	ud := l.NewUserData()
	l.Push(ud)
	l.SetMetatable(ud, l.GetTypeMetatable(metatableName))
	return 1
}
