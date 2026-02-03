package uuid

import (
	"strings"

	"github.com/google/uuid"
	lua "github.com/wippyai/go-lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/runtime/workflow"
	luaworkflow "github.com/wippyai/runtime/runtime/lua/workflow"
)

// Module is the uuid module definition.
var Module = &luaapi.ModuleDef{
	Name:        "uuid",
	Description: "UUID generation and validation",
	Class:       []string{luaapi.ClassNondeterministic, luaapi.ClassWorkflow},
	Build:       buildModule,
	Types:       ModuleTypes,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 10)
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
	return mod, []luaapi.YieldType{
		{Sample: &luaworkflow.Yield{}, CmdID: workflow.SideEffect},
	}
}

func invalidError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.Invalid).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func internalError(l *lua.LState, goErr error, context string) int {
	err := lua.WrapErrorWithLua(l, goErr, context).
		WithKind(lua.Internal).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func uuidV1(l *lua.LState) int {
	if workflow.IsDeterministic(l.Context()) {
		l.Push(luaworkflow.NewYield(func() (any, error) {
			id, err := uuid.NewUUID()
			if err != nil {
				return nil, err
			}
			return id.String(), nil
		}))
		return -1
	}
	id, err := uuid.NewUUID()
	if err != nil {
		return internalError(l, err, "v1 generation failed")
	}
	l.Push(lua.LString(id.String()))
	l.Push(lua.LNil)
	return 2
}

func uuidV3(l *lua.LState) int {
	if l.GetTop() < 2 || l.Get(1).Type() != lua.LTString || l.Get(2).Type() != lua.LTString {
		return invalidError(l, "namespace and name must be strings")
	}

	nsID, err := uuid.Parse(l.ToString(1))
	if err != nil {
		return invalidError(l, "invalid namespace UUID")
	}

	id := uuid.NewMD5(nsID, []byte(l.ToString(2)))
	l.Push(lua.LString(id.String()))
	l.Push(lua.LNil)
	return 2
}

func uuidV4(l *lua.LState) int {
	if workflow.IsDeterministic(l.Context()) {
		l.Push(luaworkflow.NewYield(func() (any, error) {
			id, err := uuid.NewRandom()
			if err != nil {
				return nil, err
			}
			return id.String(), nil
		}))
		return -1
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return internalError(l, err, "v4 generation failed")
	}
	l.Push(lua.LString(id.String()))
	l.Push(lua.LNil)
	return 2
}

func uuidV5(l *lua.LState) int {
	if l.GetTop() < 2 || l.Get(1).Type() != lua.LTString || l.Get(2).Type() != lua.LTString {
		return invalidError(l, "namespace and name must be strings")
	}

	nsID, err := uuid.Parse(l.ToString(1))
	if err != nil {
		return invalidError(l, "invalid namespace UUID")
	}

	id := uuid.NewSHA1(nsID, []byte(l.ToString(2)))
	l.Push(lua.LString(id.String()))
	l.Push(lua.LNil)
	return 2
}

func uuidV7(l *lua.LState) int {
	if workflow.IsDeterministic(l.Context()) {
		l.Push(luaworkflow.NewYield(func() (any, error) {
			id, err := uuid.NewV7()
			if err != nil {
				return nil, err
			}
			return id.String(), nil
		}))
		return -1
	}
	id, err := uuid.NewV7()
	if err != nil {
		return internalError(l, err, "v7 generation failed")
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
		return invalidError(l, "input must be a string")
	}
	id, err := uuid.Parse(l.ToString(1))
	if err != nil {
		return invalidError(l, "invalid UUID format")
	}
	l.Push(lua.LNumber(id.Version()))
	l.Push(lua.LNil)
	return 2
}

func uuidVariant(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "input must be a string")
	}
	id, err := uuid.Parse(l.ToString(1))
	if err != nil {
		return invalidError(l, "invalid UUID format")
	}

	var variant string
	switch id.Variant() {
	case uuid.RFC4122:
		variant = "RFC4122"
	case uuid.Reserved:
		variant = "Reserved"
	case uuid.Invalid:
		variant = "Invalid"
	case uuid.Microsoft:
		variant = "Microsoft"
	case uuid.Future:
		variant = "Future"
	default:
		variant = "NCS"
	}
	l.Push(lua.LString(variant))
	l.Push(lua.LNil)
	return 2
}

func uuidParse(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		return invalidError(l, "input must be a string")
	}
	id, err := uuid.Parse(l.ToString(1))
	if err != nil {
		return invalidError(l, "invalid UUID format")
	}

	tbl := l.CreateTable(0, 4)
	tbl.RawSetString("version", lua.LNumber(id.Version()))

	var variant string
	switch id.Variant() {
	case uuid.RFC4122:
		variant = "RFC4122"
	case uuid.Reserved:
		variant = "Reserved"
	case uuid.Invalid:
		variant = "Invalid"
	case uuid.Microsoft:
		variant = "Microsoft"
	case uuid.Future:
		variant = "Future"
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
		return invalidError(l, "input must be a string")
	}

	format := "standard"
	if l.Get(2).Type() == lua.LTString {
		format = l.ToString(2)
	}

	id, err := uuid.Parse(l.ToString(1))
	if err != nil {
		return invalidError(l, "invalid UUID format")
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
		return invalidError(l, "unsupported format")
	}

	l.Push(lua.LString(result))
	l.Push(lua.LNil)
	return 2
}
