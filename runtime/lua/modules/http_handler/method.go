package httphandler

import "github.com/ponyruntime/go-lua"

// we can use it outside of the module structure
func (m *Module) method(l *lua.LState) int {
	m.log.Debug("called http handler 'method' function")

	ud := l.CheckUserData(1)
	if ud == nil {
		l.ArgError(1, "expected userdata for http handler")
		return 0
	}

	httph, ok := ud.Value.(*httpHandler)
	if !ok {
		l.ArgError(1, "invalid userdata type for http handler")
		return 0
	}

	if httph == nil {
		l.ArgError(1, "http handler is nil")
		return 0
	}

	if httph.r == nil {
		l.ArgError(1, "http request is nil")
		return 0
	}

	l.Push(lua.LString(httph.r.Method))
	return 1
}
