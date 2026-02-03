package template

import (
	"errors"
	"fmt"
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	servicetemplate "github.com/wippyai/runtime/service/template"
	"github.com/wippyai/runtime/service/template/jet"
)

const typeTemplateSet = "template.Set"

// Module is the template module definition.
var Module = &luaapi.ModuleDef{
	Name:        "templates",
	Description: "Template rendering engine",
	Class:       []string{luaapi.ClassDeterministic},
	Build:       buildModule,
	Types:       ModuleTypes,
}

func init() {
	value.RegisterTypeMethods(nil, typeTemplateSet,
		map[string]lua.LGoFunc{"__tostring": templateSetToString},
		map[string]lua.LGoFunc{
			"render":  templateSetRender,
			"release": templateSetRelease,
		})
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 1)
	mod.RawSetString("get", lua.LGoFunc(templateGet))
	mod.Immutable = true
	return mod, nil
}

// Set wraps jet.Set for Lua.
type Set struct {
	resource  resource.Resource[any]
	templates *jet.Set
	released  bool
	mu        sync.Mutex
}

func pushError(l *lua.LState, err *lua.Error) int {
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func templateGet(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return pushError(l, lua.NewLuaError(l, "no context").
			WithKind(lua.Internal).
			WithRetryable(false))
	}

	id := l.CheckString(1)
	if id == "" {
		return pushError(l, lua.NewLuaError(l, "resource id is required").
			WithKind(lua.Invalid).
			WithRetryable(false))
	}

	if !security.IsAllowed(ctx, "template.get", id, nil) {
		return pushError(l, lua.NewLuaError(l, fmt.Sprintf("not allowed to access template: %s", id)).
			WithKind(lua.PermissionDenied).
			WithRetryable(false))
	}

	reg := resource.GetRegistry(ctx)
	if reg == nil {
		return pushError(l, lua.NewLuaError(l, "resource registry not found").
			WithKind(lua.Internal).
			WithRetryable(false))
	}

	resID := registry.ParseID(id)
	res, acquireErr := reg.Acquire(ctx, resID, resource.ModeNormal)
	if acquireErr != nil {
		err := lua.WrapErrorWithLua(l, acquireErr, "failed to acquire resource").
			WithKind(lua.Internal).
			WithRetryable(false)
		return pushError(l, err)
	}

	templateRes, getErr := res.Get()
	if getErr != nil {
		res.Release()
		err := lua.WrapErrorWithLua(l, getErr, "failed to get resource").
			WithKind(lua.Internal).
			WithRetryable(false)
		return pushError(l, err)
	}

	templateSet, ok := templateRes.(*jet.Set)
	if !ok {
		res.Release()
		return pushError(l, lua.NewLuaError(l, fmt.Sprintf("resource is not a template set: %T", templateRes)).
			WithKind(lua.Internal).
			WithRetryable(false))
	}

	ts := &Set{
		resource:  res,
		templates: templateSet,
	}

	value.PushTypedUserData(l, ts, typeTemplateSet)
	return 1
}

func templateSetRender(l *lua.LState) int {
	ts := checkTemplateSet(l, 1)
	if ts == nil {
		return 0
	}

	ts.mu.Lock()
	released := ts.released
	templates := ts.templates
	ts.mu.Unlock()

	if released {
		return pushError(l, lua.NewLuaError(l, "template set is released").
			WithKind(lua.Internal).
			WithRetryable(false))
	}

	name := l.CheckString(2)
	if name == "" {
		return pushError(l, lua.NewLuaError(l, "template name is required").
			WithKind(lua.Invalid).
			WithRetryable(false))
	}

	args := l.OptTable(3, lua.CreateTable(0, 0))

	result, renderErr := templates.RenderPayload(name, payload.NewPayload(args, payload.Lua))
	if renderErr != nil {
		if errors.Is(renderErr, servicetemplate.ErrTemplateNotFound) {
			return pushError(l, lua.NewLuaError(l, "template not found").
				WithKind(lua.NotFound).
				WithRetryable(false))
		}
		err := lua.WrapErrorWithLua(l, renderErr, "failed to render template").
			WithKind(lua.Internal).
			WithRetryable(false)
		return pushError(l, err)
	}

	l.Push(lua.LString(result))
	l.Push(lua.LNil)
	return 2
}

func templateSetRelease(l *lua.LState) int {
	ts := checkTemplateSet(l, 1)
	if ts == nil {
		return 0
	}

	ts.mu.Lock()
	if !ts.released && ts.resource != nil {
		ts.resource.Release()
		ts.resource = nil
		ts.released = true
	}
	ts.mu.Unlock()

	l.Push(lua.LTrue)
	return 1
}

func templateSetToString(l *lua.LState) int {
	ts := checkTemplateSet(l, 1)
	if ts == nil {
		return 0
	}

	ts.mu.Lock()
	released := ts.released
	ts.mu.Unlock()

	if released {
		l.Push(lua.LString("template.Set{released}"))
	} else {
		l.Push(lua.LString("template.Set{}"))
	}
	return 1
}

func checkTemplateSet(l *lua.LState, idx int) *Set {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*Set); ok {
		return v
	}
	l.ArgError(idx, "template.Set expected")
	return nil
}
