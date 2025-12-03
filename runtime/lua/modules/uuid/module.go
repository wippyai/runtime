package uuid

import (
	"strings"
	"sync"

	"github.com/google/uuid"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

var (
	moduleTable  *lua.LTable
	registration *luaapi.Registration
	initOnce     sync.Once
)

// Module is the singleton uuid module instance.
var Module = &uuidModule{}

type uuidModule struct{}

func (m *uuidModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "uuid",
		Description: "UUID generation and validation",
		Class:       []string{luaapi.ClassNondeterministic},
	}
}

func (m *uuidModule) Register(l *lua.LState) *luaapi.Registration {
	initOnce.Do(func() {
		mod := &lua.LTable{}
		mod.RawSetString("v1", lua.LGoFunc(uuidV1))
		mod.RawSetString("v3", lua.LGoFunc(uuidV3))
		mod.RawSetString("v4", lua.LGoFunc(uuidV4))
		mod.RawSetString("v5", lua.LGoFunc(uuidV5))
		mod.RawSetString("v7", lua.LGoFunc(uuidV7))
		mod.RawSetString("validate", lua.LGoFunc(uuidValidate))
		mod.RawSetString("version", lua.LGoFunc(uuidVersion))
		mod.RawSetString("variant", lua.LGoFunc(uuidVariant))
		mod.RawSetString("parse", lua.LGoFunc(uuidParse))
		mod.RawSetString("format", lua.LGoFunc(uuidFormat))
		mod.Immutable = true
		moduleTable = mod

		registration = &luaapi.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})
	return registration
}

func (m *uuidModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use luaapi.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	luaapi.LoadModule(l, Module)
}

func uuidV1(l *lua.LState) int {
	id, err := uuid.NewUUID()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LString(id.String()))
	l.Push(lua.LNil)
	return 2
}

func uuidV3(l *lua.LState) int {
	if l.GetTop() < 2 || l.Get(1).Type() != lua.LTString || l.Get(2).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("namespace and name must be strings"))
		return 2
	}

	nsID, err := uuid.Parse(l.ToString(1))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid namespace UUID"))
		return 2
	}

	id := uuid.NewMD5(nsID, []byte(l.ToString(2)))
	l.Push(lua.LString(id.String()))
	l.Push(lua.LNil)
	return 2
}

func uuidV4(l *lua.LState) int {
	id, err := uuid.NewRandom()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LString(id.String()))
	l.Push(lua.LNil)
	return 2
}

func uuidV5(l *lua.LState) int {
	if l.GetTop() < 2 || l.Get(1).Type() != lua.LTString || l.Get(2).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("namespace and name must be strings"))
		return 2
	}

	nsID, err := uuid.Parse(l.ToString(1))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid namespace UUID"))
		return 2
	}

	id := uuid.NewSHA1(nsID, []byte(l.ToString(2)))
	l.Push(lua.LString(id.String()))
	l.Push(lua.LNil)
	return 2
}

func uuidV7(l *lua.LState) int {
	id, err := uuid.NewV7()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LString(id.String()))
	l.Push(lua.LNil)
	return 2
}

func uuidValidate(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LFalse)
		l.Push(lua.LNil)
		return 2
	}
	_, err := uuid.Parse(l.ToString(1))
	l.Push(lua.LBool(err == nil))
	l.Push(lua.LNil)
	return 2
}

func uuidVersion(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("input must be a string"))
		return 2
	}
	id, err := uuid.Parse(l.ToString(1))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid UUID format"))
		return 2
	}
	l.Push(lua.LNumber(id.Version()))
	l.Push(lua.LNil)
	return 2
}

func uuidVariant(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("input must be a string"))
		return 2
	}
	id, err := uuid.Parse(l.ToString(1))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid UUID format"))
		return 2
	}

	var variant string
	switch id.Variant() {
	case uuid.RFC4122:
		variant = "RFC4122"
	case uuid.Reserved:
		variant = "Microsoft"
	case uuid.Invalid:
		variant = "Invalid"
	default:
		variant = "NCS"
	}
	l.Push(lua.LString(variant))
	l.Push(lua.LNil)
	return 2
}

func uuidParse(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("input must be a string"))
		return 2
	}
	id, err := uuid.Parse(l.ToString(1))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid UUID format"))
		return 2
	}

	tbl := l.CreateTable(0, 4)
	tbl.RawSetString("version", lua.LNumber(id.Version()))

	var variant string
	switch id.Variant() {
	case uuid.RFC4122:
		variant = "RFC4122"
	case uuid.Reserved:
		variant = "Microsoft"
	case uuid.Invalid:
		variant = "Invalid"
	default:
		variant = "NCS"
	}
	tbl.RawSetString("variant", lua.LString(variant))

	switch id.Version() {
	case 1:
		sec, _ := id.Time().UnixTime()
		tbl.RawSetString("timestamp", lua.LNumber(sec))
		tbl.RawSetString("node", lua.LString(id.NodeID()))
	case 7:
		sec, _ := id.Time().UnixTime()
		tbl.RawSetString("timestamp", lua.LNumber(sec))
	}

	l.Push(tbl)
	l.Push(lua.LNil)
	return 2
}

func uuidFormat(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("input must be a string"))
		return 2
	}

	format := "standard"
	if l.Get(2).Type() == lua.LTString {
		format = l.ToString(2)
	}

	id, err := uuid.Parse(l.ToString(1))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid UUID format"))
		return 2
	}

	var result string
	switch format {
	case "simple":
		result = strings.ReplaceAll(id.String(), "-", "")
	case "urn":
		result = "urn:uuid:" + id.String()
	case "standard":
		result = id.String()
	default:
		l.Push(lua.LNil)
		l.Push(lua.LString("unsupported format"))
		return 2
	}

	l.Push(lua.LString(result))
	l.Push(lua.LNil)
	return 2
}
