// SPDX-License-Identifier: MPL-2.0

package security

import (
	lua "github.com/wippyai/go-lua"
	secapi "github.com/wippyai/runtime/api/security"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const actorTypeName = "security.Actor"

var actorMethods = map[string]lua.LGoFunc{
	"id":   actorID,
	"meta": actorMeta,
}

func wrapActor(l *lua.LState, actor secapi.Actor) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = actor
	ud.Metatable = value.GetTypeMetatable(l, actorTypeName)
	return ud
}

func checkActor(l *lua.LState, idx int) secapi.Actor {
	ud := l.CheckUserData(idx)
	if actor, ok := ud.Value.(secapi.Actor); ok {
		return actor
	}
	l.ArgError(idx, "Actor expected")
	return secapi.Actor{}
}

func actorID(l *lua.LState) int {
	actor := checkActor(l, 1)
	l.Push(lua.LString(actor.ID))
	return 1
}

func actorMeta(l *lua.LState) int {
	actor := checkActor(l, 1)
	tbl := lua.CreateTable(0, len(actor.Meta))
	for k, v := range actor.Meta {
		lv, err := luaconv.GoToLua(v)
		if err != nil {
			lv = lua.LNil
		}
		tbl.RawSetString(k, lv)
	}
	l.Push(tbl)
	return 1
}
