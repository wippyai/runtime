package yaml

import (
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
	"gopkg.in/yaml.v3"
)

var (
	moduleTable  *lua.LTable
	registration *luaapi.Registration
	initOnce     sync.Once
)

// Module is the singleton yaml module instance.
var Module = &yamlModule{}

type yamlModule struct{}

func (m *yamlModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "yaml",
		Description: "YAML encoding and decoding",
		Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	}
}

func (m *yamlModule) Register(l *lua.LState) *luaapi.Registration {
	initOnce.Do(func() {
		mod := &lua.LTable{}
		mod.RawSetString("encode", lua.LGoFunc(encode))
		mod.RawSetString("decode", lua.LGoFunc(decode))
		mod.Immutable = true
		moduleTable = mod

		registration = &luaapi.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})
	return registration
}

func (m *yamlModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use luaapi.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	luaapi.LoadModule(l, Module)
}

func encode(l *lua.LState) int {
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("missing input table"))
		return 2
	}

	luaVal := l.Get(1)
	if luaVal.Type() != lua.LTTable {
		l.Push(lua.LNil)
		l.Push(lua.LString("first argument must be a table"))
		return 2
	}

	goVal := value.ToGoAny(luaVal)

	data, err := yaml.Marshal(goVal)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(data))
	l.Push(lua.LNil)
	return 2
}

func decode(l *lua.LState) int {
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("missing input YAML string"))
		return 2
	}

	yamlStr := l.CheckString(1)
	if yamlStr == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("input cannot be empty"))
		return 2
	}

	var data interface{}
	if err := yaml.Unmarshal([]byte(yamlStr), &data); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	lv, err := luaconv.GoToLua(data)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lv)
	l.Push(lua.LNil)
	return 2
}
