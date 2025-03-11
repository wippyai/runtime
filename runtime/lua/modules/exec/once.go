package exec

import (
	"sync"

	lua "github.com/yuin/gopher-lua"
)

type onceStream struct {
	once  sync.Once
	value *lua.LUserData
}

func newOnceStream() *onceStream {
	return &onceStream{}
}

func (o *onceStream) Do(f func() *lua.LUserData) *lua.LUserData {
	o.once.Do(func() {
		o.value = f()
	})

	return o.value
}
