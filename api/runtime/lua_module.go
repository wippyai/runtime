package runtime

import "github.com/ponyruntime/go-lua"

type LuaModule interface {
	Loader(*lua.LState) int
	Name() string
}
