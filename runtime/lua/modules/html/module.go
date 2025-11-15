package html

import (
	"sync"

	lua "github.com/yuin/gopher-lua"
)

type Module struct {
	moduleTable *lua.LTable
	once        sync.Once
}

func NewHTMLModule() *Module {
	return &Module{}
}

func (m *Module) Name() string {
	return "html"
}

func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		m.initModuleTable(l)
	})

	l.Push(m.moduleTable)
	return 1
}

func (m *Module) initModuleTable(l *lua.LState) {
	t := l.CreateTable(0, 1)

	sanitizeMod := l.CreateTable(0, 3)
	sanitizeMod.RawSetString("new_policy", l.NewFunction(newPolicy))
	sanitizeMod.RawSetString("ugc_policy", l.NewFunction(ugcPolicy))
	sanitizeMod.RawSetString("strict_policy", l.NewFunction(strictPolicy))
	sanitizeMod.Immutable = true
	t.RawSetString("sanitize", sanitizeMod)

	registerPolicy(l)
	registerAttrBuilder(l)

	t.Immutable = true
	m.moduleTable = t
}
