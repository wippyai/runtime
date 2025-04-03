package template

import (
	"context"
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/service/template"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module represents a template Lua module
type Module struct {
	log *zap.Logger
}

// NewTemplateModule creates and returns a new instance of the template Module
func NewTemplateModule(log *zap.Logger) *Module {
	return &Module{
		log: log,
	}
}

// Name returns the module's name
func (m *Module) Name() string {
	return "templates"
}

// Loader loads the module into the given Lua state
func (m *Module) Loader(l *lua.LState) int {
	// Create a module table with the get function
	mod := l.CreateTable(0, 1)

	// Register get function
	mod.RawSetString("get", l.NewFunction(func(l *lua.LState) int {
		return templateGet(l, m.log)
	}))

	// Register Template type methods
	registerTemplateSet(l)

	// Push the module table
	l.Push(mod)
	return 1
}

// Wrapper represents a template set wrapper for Lua
type Wrapper struct {
	resource  resource.Resource[any]
	templates *template.TemplateSet
	log       *zap.Logger
	onRelease context.CancelFunc
}

// NewWrapper creates a new template set wrapper with UoW integration
func NewWrapper(
	uw engine.UnitOfWork,
	resource resource.Resource[any],
	templates *template.TemplateSet,
	log *zap.Logger,
) *Wrapper {
	wrapper := &Wrapper{
		resource:  resource,
		templates: templates,
		log:       log,
	}

	// Register unconditional cleanup in UoW
	wrapper.onRelease = uw.AddCleanup(func() error {
		resource.Release()
		return nil
	})

	return wrapper
}

// registerTemplateSet registers the TemplateSet type and its methods
func registerTemplateSet(l *lua.LState) {
	methods := map[string]lua.LGFunction{
		"render":  templateRender,
		"release": templateRelease,
	}

	value.RegisterMethods(l, "template.Set", methods)
}

// CheckTemplateSet checks if the first argument is a Wrapper and returns it
func CheckTemplateSet(l *lua.LState) *Wrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*Wrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected template set object")
	return nil
}

// WrapTemplateSet wraps a TemplateSet as a Lua userdata
func WrapTemplateSet(l *lua.LState, wrapper *Wrapper) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = wrapper
	ud.Metatable = value.GetTypeMetatable(l, "template.Set")
	return ud
}

// templateGet retrieves a template set resource by ID
func templateGet(l *lua.LState, log *zap.Logger) int {
	// Get resource ID
	id := l.CheckString(1)
	if id == "" {
		l.RaiseError("resource ID is required")
		return 0
	}

	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work found in context")
		return 0
	}

	reg := resource.GetResources(uw.Context())
	if reg == nil {
		l.RaiseError("resource registry not found")
		return 0
	}

	// Parse resource ID
	resID := registry.ParseID(id)

	// Acquire resource
	res, err := reg.Acquire(uw.Context(), resID, resource.ModeNormal)
	if err != nil {
		l.RaiseError("failed to acquire resource: %v", err)
		return 0
	}

	// Get TemplateSet instance
	templateRes, err := res.Get()
	if err != nil {
		res.Release()
		l.RaiseError("failed to get resource: %v", err)
		return 0
	}

	// Check if it's a TemplateSet implementation
	templateSet, ok := templateRes.(*template.TemplateSet)
	if !ok {
		res.Release()
		l.RaiseError("resource is not a template set: %T", templateRes)
		return 0
	}

	// Create and wrap TemplateSet with UoW integration
	wrapper := NewWrapper(uw, res, templateSet, log)

	// Create userdata
	ud := WrapTemplateSet(l, wrapper)
	l.Push(ud)
	return 1
}

// templateRender renders a template by name with variables
func templateRender(l *lua.LState) int {
	// Check and get template set
	wrapper := CheckTemplateSet(l)
	if wrapper == nil {
		return 0
	}

	// Get template name
	name := l.CheckString(2)
	if name == "" {
		l.RaiseError("template name is required")
		return 0
	}

	// Get
	args := l.OptTable(3, l.CreateTable(0, 0))

	// Render the template
	result, err := wrapper.templates.RenderPayload(name, payload.NewPayload(args, payload.Lua))
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

// templateRelease releases a template set resource
func templateRelease(l *lua.LState) int {
	// Check and get template wrapper
	wrapper := CheckTemplateSet(l)
	if wrapper == nil {
		return 0
	}

	// Release the resource directly
	if wrapper.resource != nil {
		wrapper.resource.Release()
		wrapper.resource = nil
	}

	// Cancel the cleanup function in UoW (don't execute it, just remove it)
	if wrapper.onRelease != nil {
		wrapper.onRelease()
		wrapper.onRelease = nil
	}

	l.Push(lua.LTrue)
	return 1
}
