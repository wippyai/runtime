package security

import (
	"github.com/ponyruntime/pony/api/payload"
	secapi "github.com/ponyruntime/pony/api/security"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

const ActorMetatable = "security.Actor"

// wrapActor wraps a security.Actor as a Lua userdata
func wrapActor(l *lua.LState, actor secapi.Actor) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = actor
	ud.Metatable = value.GetTypeMetatable(l, ActorMetatable)
	return ud
}

// checkActor checks if the first argument is an Actor and returns it
func checkActor(l *lua.LState) secapi.Actor {
	ud := l.CheckUserData(1)
	if actor, ok := ud.Value.(secapi.Actor); ok {
		return actor
	}
	l.ArgError(1, "Actor expected")
	return secapi.Actor{}
}

// registerActorType registers the Actor type and methods
func registerActorType(l *lua.LState) {
	value.RegisterMethods(l, ActorMetatable, map[string]lua.LGFunction{
		"id":   actorID,
		"meta": actorMeta,
	})
}

// actorID returns the actor's id
func actorID(l *lua.LState) int {
	actor := checkActor(l)
	l.Push(lua.LString(actor.ID))
	return 1
}

// actorMeta returns the actor's metadata
func actorMeta(l *lua.LState) int {
	actor := checkActor(l)

	dtt := payload.GetTranscoder(l.Context())
	if dtt == nil {
		l.RaiseError("no transcoder registered for payload format")
		return 0
	}

	// Convert metadata to Lua table
	metaTable := l.CreateTable(0, len(actor.Meta))
	for k, v := range actor.Meta {
		val, err := luaconv.GoToLua(v)
		if err != nil {
			l.RaiseError("error converting metadata value to Lua: %v", err)
			return 0
		}
		metaTable.RawSetString(k, val)
	}

	l.Push(metaTable)
	return 1
}
