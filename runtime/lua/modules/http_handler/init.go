package httphandler

import (
	"net/http"

	"github.com/ponyruntime/go-lua"
)

// new - initializes the HTTP handler module, no args expected
func (m *Module) new(l *lua.LState) int {
	m.log.Debug("called new")

	ud := l.NewUserData()
	ud.Value = m.handler

	l.SetMetatable(ud, l.GetTypeMetatable(metatableName))
	l.Push(ud)

	return 1
}

func (m *Module) Init(r *http.Request, rw http.ResponseWriter) {
	m.log.Debug("http_handler module initialized")

	m.handler = &httpHandler{
		r:  r,
		rw: rw,
	}
}
