package template

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua2api "github.com/wippyai/runtime/api/runtime/lua2"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	"github.com/wippyai/runtime/service/template"
	lua "github.com/yuin/gopher-lua"
)

const templateSetTypeName = "template.Set"

var (
	moduleTable          *lua.LTable
	registration         *lua2api.Registration
	templateSetMetatable *lua.LTable
	initOnce             sync.Once
)

// Module is the singleton template module instance.
var Module = &templateModule{}

type templateModule struct{}

func (m *templateModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "templates",
		Description: "Template rendering engine",
		Class:       []string{luaapi.ClassDeterministic},
	}
}

func (m *templateModule) Register(l *lua.LState) *lua2api.Registration {
	initOnce.Do(func() {
		mod := lua.CreateTable(0, 1)
		mod.RawSetString("get", lua.LGoFunc(templateGet))
		mod.Immutable = true
		moduleTable = mod

		templateSetMetatable = value.RegisterTypeMethods(nil, templateSetTypeName,
			map[string]lua.LGFunction{"__tostring": templateSetToString},
			templateSetMethods)

		registration = &lua2api.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})

	return registration
}

func (m *templateModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use lua2api.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	lua2api.LoadModule(l, Module)
}

// TemplateSet wraps template.TemplateSet with cleanup tracking.
type TemplateSet struct {
	resource      resource.Resource[any]
	templates     *template.TemplateSet
	released      bool
	mu            sync.Mutex
	cancelCleanup func()
}

func NewTemplateSet(ctx context.Context, res resource.Resource[any], templates *template.TemplateSet) *TemplateSet {
	ts := &TemplateSet{
		resource:  res,
		templates: templates,
		released:  false,
	}

	resources := engine.GetResources(ctx)
	if resources != nil {
		ts.cancelCleanup = resources.AddCleanup(func() error {
			ts.mu.Lock()
			defer ts.mu.Unlock()
			if !ts.released && ts.resource != nil {
				ts.resource.Release()
				ts.released = true
			}
			return nil
		})
	}

	return ts
}

var templateSetMethods = map[string]lua.LGFunction{
	"render":  templateSetRender,
	"release": templateSetRelease,
}

func checkTemplateSet(l *lua.LState, idx int) *TemplateSet {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*TemplateSet); ok {
		return v
	}
	l.ArgError(idx, "template.Set expected")
	return nil
}

func templateGet(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	id := l.CheckString(1)
	if id == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("resource id is required"))
		return 2
	}

	if !security.IsAllowed(ctx, "template.get", id, nil) {
		l.RaiseError("not allowed to access template: %s", id)
		return 0
	}

	reg := resource.GetRegistry(ctx)
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("resource registry not found"))
		return 2
	}

	resID := registry.ParseID(id)
	res, err := reg.Acquire(ctx, resID, resource.ModeNormal)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to acquire resource: %v", err)))
		return 2
	}

	templateRes, err := res.Get()
	if err != nil {
		res.Release()
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to get resource: %v", err)))
		return 2
	}

	templateSet, ok := templateRes.(*template.TemplateSet)
	if !ok {
		res.Release()
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("resource is not a template set: %T", templateRes)))
		return 2
	}

	ts := NewTemplateSet(ctx, res, templateSet)

	value.NewUserData(l, ts, templateSetMetatable)
	return 1
}

func templateSetRender(l *lua.LState) int {
	ts := checkTemplateSet(l, 1)
	if ts == nil {
		return 0
	}

	ts.mu.Lock()
	if ts.released {
		ts.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.LString("template set is released"))
		return 2
	}
	templates := ts.templates
	ts.mu.Unlock()

	name := l.CheckString(2)
	if name == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("template name is required"))
		return 2
	}

	args := l.OptTable(3, lua.CreateTable(0, 0))

	result, err := templates.RenderPayload(name, payload.NewPayload(args, payload.Lua))
	if err != nil {
		if errors.Is(err, template.ErrTemplateNotFound) {
			l.Push(lua.LNil)
			l.Push(lua.LString("template not found"))
			return 2
		}
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to render template: %v", err)))
		return 2
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
		cancel := ts.cancelCleanup
		ts.cancelCleanup = nil
		ts.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	} else {
		ts.mu.Unlock()
	}

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
