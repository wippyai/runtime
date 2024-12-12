package httphandler

import "github.com/ponyruntime/go-lua"

// we can use it outside of the module structure
// expected userdata for http handler: string with data
func (m *Module) write(l *lua.LState) int {
	ud := l.CheckUserData(1)
	httph, err := checkUserData(ud)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	data := l.CheckString(1)
	_, err = httph.rw.Write([]byte(data))
	if err != nil {
		l.Push(lua.LString("failed to write data: " + err.Error()))
		return 1
	}

	l.Push(lua.LNil)
	return 1
}
