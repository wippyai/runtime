package httphandler

import "github.com/ponyruntime/go-lua"

// we can use it outside of the module structure
// expected userdata for http handler: string with data
func (m *Module) write(l *lua.LState) int {
	m.log.Debug("called http handler 'write' function")
	ctx := l.Context()

	carrier := m.FromContext(ctx)
	if carrier == nil {
		l.Push(lua.LString("no context found"))
		return 1
	}

	data := l.CheckString(1)
	_, err := carrier.ResponseWriter().Write([]byte(data))
	if err != nil {
		l.Push(lua.LString("failed to write data: " + err.Error()))
		return 1
	}

	l.Push(lua.LNil)
	return 1
}
