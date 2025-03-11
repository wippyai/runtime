package uuid

import (
	"fmt"
	"github.com/google/uuid"
	lua "github.com/yuin/gopher-lua"
	"strings"
)

// Module represents a UUID Lua module.
type Module struct{}

// NewUUIDModule creates and returns a new instance of the UUID Module.
func NewUUIDModule() *Module {
	return &Module{}
}

// Name returns the module's name.
func (m *Module) Name() string {
	return "uuid"
}

// Loader registers the module's functions into Lua state.
func (m *Module) Loader(l *lua.LState) int {
	// Create a module table with exact pre-allocated size
	mod := l.CreateTable(0, 10) // Exactly 10 functions

	// Register functions using RawSetString for better performance
	mod.RawSetString("v1", l.NewFunction(m.v1))
	mod.RawSetString("v3", l.NewFunction(m.v3))
	mod.RawSetString("v4", l.NewFunction(m.v4))
	mod.RawSetString("v5", l.NewFunction(m.v5))
	mod.RawSetString("v7", l.NewFunction(m.v7))
	mod.RawSetString("validate", l.NewFunction(m.validate))
	mod.RawSetString("version", l.NewFunction(m.version))
	mod.RawSetString("variant", l.NewFunction(m.variant))
	mod.RawSetString("parse", l.NewFunction(m.parse))
	mod.RawSetString("format", l.NewFunction(m.format))

	l.Push(mod)
	return 1
}

// v4 generates a random UUID.
func (*Module) v4(l *lua.LState) int {
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

// v7 generates a time-ordered UUID.
func (*Module) v7(l *lua.LState) int {
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

// v1 generates a time-based UUID.
func (*Module) v1(l *lua.LState) int {
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

// v3 generates an MD5 namespace UUID.
func (*Module) v3(l *lua.LState) int {
	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("namespace must be a string"))
		return 2
	}

	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("namespace must be a string"))
		return 2
	}
	if l.Get(2).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("name must be a string"))
		return 2
	}

	namespace := l.ToString(1)
	name := l.ToString(2)

	nsID, err := uuid.Parse(namespace)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid namespace UUID"))
		return 2
	}

	id := uuid.NewMD5(nsID, []byte(name))
	l.Push(lua.LString(id.String()))
	l.Push(lua.LNil)
	return 2
}

// v5 generates a SHA1 namespace UUID.
func (*Module) v5(l *lua.LState) int {
	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("namespace must be a string"))
		return 2
	}

	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("namespace must be a string"))
		return 2
	}
	if l.Get(2).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("name must be a string"))
		return 2
	}

	namespace := l.ToString(1)
	name := l.ToString(2)

	nsID, err := uuid.Parse(namespace)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid namespace UUID"))
		return 2
	}

	id := uuid.NewSHA1(nsID, []byte(name))
	l.Push(lua.LString(id.String()))
	l.Push(lua.LNil)
	return 2
}

// validate checks if a string is a valid UUID.
func (*Module) validate(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("input must be a string"))
		return 2
	}

	str := l.ToString(1)
	_, err := uuid.Parse(str)
	l.Push(lua.LBool(err == nil))
	l.Push(lua.LNil)
	return 2
}

// version gets the version of a UUID.
func (*Module) version(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("input must be a string"))
		return 2
	}

	str := l.ToString(1)
	id, err := uuid.Parse(str)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid UUID format"))
		return 2
	}

	l.Push(lua.LNumber(id.Version()))
	l.Push(lua.LNil)
	return 2
}

// variant gets the variant of a UUID.
func (*Module) variant(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("input must be a string"))
		return 2
	}

	str := l.ToString(1)
	id, err := uuid.Parse(str)
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

// parse breaks down a UUID into its components.
func (*Module) parse(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("input must be a string"))
		return 2
	}

	str := l.ToString(1)
	id, err := uuid.Parse(str)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid UUID format"))
		return 2
	}

	// Pre-allocate table with maximum possible size
	// Maximum fields: version, variant, timestamp, node = 4
	tbl := l.CreateTable(0, 4)

	// Add version and variant
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

	// For v1 and v7, add timestamp
	switch id.Version() {
	case 1:
		sec, _ := id.Time().UnixTime()
		tbl.RawSetString("timestamp", lua.LNumber(sec))
		tbl.RawSetString("node", lua.LString(fmt.Sprintf("%x", id.NodeID())))
	case 7:
		sec, _ := id.Time().UnixTime()
		tbl.RawSetString("timestamp", lua.LNumber(sec))
	}

	l.Push(tbl)
	l.Push(lua.LNil)
	return 2
}

// format formats a UUID in different representations.
func (*Module) format(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("input must be a string"))
		return 2
	}

	str := l.ToString(1)
	format := "standard"
	if l.Get(2).Type() == lua.LTString {
		format = l.ToString(2)
	}

	id, err := uuid.Parse(str)
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
