package engine

import (
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/modules/ostime"
	"github.com/wippyai/runtime/runtime/lua/modules/payload"
	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/inspect"
	"go.uber.org/zap"
)

// ChannelModule provides Go-style channels for Lua.
var ChannelModule = &luaapi.ModuleDef{
	Name:        "channel",
	Description: "Go-style channels for inter-coroutine communication",
	Class:       []string{luaapi.ClassProcess},
	Build:       buildChannelModule,
	Types:       ChannelModuleTypes,
}

func init() {
	value.RegisterTypeMethods(nil, ChannelTypeName, nil, channelMethods)
}

func buildChannelModule() (*lua.LTable, []luaapi.YieldType) {
	tbl := lua.CreateTable(0, 2)
	tbl.RawSetString("new", lua.LGoFunc(channelNewFunc))
	tbl.RawSetString("select", lua.LGoFunc(channelSelectFunc))
	tbl.Immutable = true
	return tbl, nil
}

// PubSubModule provides subscribe/unsubscribe global functions.
var PubSubModule = &luaapi.ModuleDef{
	Name:        "pubsub",
	Description: "Pub/sub channel subscription functions",
	Class:       []string{luaapi.ClassProcess},
	Build:       buildPubSubModule,
}

func buildPubSubModule() (*lua.LTable, []luaapi.YieldType) {
	tbl := lua.CreateTable(0, 2)
	tbl.RawSetString("subscribe", lua.LGoFunc(subscribeFunc))
	tbl.RawSetString("unsubscribe", lua.LGoFunc(unsubscribeFunc))
	tbl.Immutable = true
	return tbl, nil
}

// PrintModule provides the custom print function.
var PrintModule = &luaapi.ModuleDef{
	Name:        "print",
	Description: "Custom print function with logging support",
	Class:       []string{luaapi.ClassIO},
	BuildValue:  buildPrintModule,
}

func buildPrintModule() (lua.LValue, []luaapi.YieldType) {
	return lua.LGoFunc(printFunc), nil
}

// printFunc is the implementation of the custom print function.
func printFunc(l *lua.LState) int {
	log := logs.GetLogger(l.Context())

	parts := make([]string, l.GetTop())
	for i := 1; i <= l.GetTop(); i++ {
		parts[i-1] = l.ToString(i)
	}
	msg := strings.Join(parts, " ")

	if log == nil {
		fmt.Print(msg)
		return 0
	}

	fields := make([]zap.Field, 0, 2)

	if pid, ok := runtime.GetFramePID(l.Context()); ok {
		fields = append(fields, zap.String("pid", pid.String()))
	}

	if id, ok := runtime.GetFrameID(l.Context()); ok {
		if line, ok := inspect.GetCallerLine(l, 1); ok {
			location := fmt.Sprintf("%s:%d", id.String(), line)
			fields = append(fields, zap.String("location", location))
		}
	}

	log.Info(msg, fields...)
	return 0
}

// LoadCoreModules loads all core modules into the LState.
func LoadCoreModules(l *lua.LState) {
	lua.OpenErrors(l)
	payload.Module.Load(l)
	ostime.Module.Load(l)
	PrintModule.Load(l)
	ChannelModule.Load(l)
	loadPubSubGlobals(l)
}

// loadPubSubGlobals loads subscribe/unsubscribe as global functions.
func loadPubSubGlobals(l *lua.LState) {
	PubSubModule.Register(l)
	l.SetGlobal("subscribe", lua.LGoFunc(subscribeFunc))
	l.SetGlobal("unsubscribe", lua.LGoFunc(unsubscribeFunc))
}

// CoreBinders returns the core module loader as a single binder.
func CoreBinders() []ModuleBinder {
	return []ModuleBinder{LoadCoreModules}
}
