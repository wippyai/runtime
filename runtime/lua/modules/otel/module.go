package otel

import (
	"context"

	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Module provides Lua bindings for OpenTelemetry functionality
type Module struct {
}

// NewOTelModule creates a new OpenTelemetry module
func NewOTelModule() *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "otel"
}

// Loader is the entry point for loading the module into Lua
func (m *Module) Loader(l *lua.LState) int {
	t := l.CreateTable(0, 3) // Exactly 3 functions: attribute, event, status

	t.RawSetString("attribute", l.NewFunction(m.attribute))
	t.RawSetString("event", l.NewFunction(m.event))
	t.RawSetString("status", l.NewFunction(m.status))
	l.Push(t)
	return 1
}

// getCurrentSpan retrieves the current span from the context
func getCurrentSpan(ctx context.Context) trace.Span {
	if ctx == nil {
		return nil
	}

	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return nil
	}

	return trace.SpanFromContext(ctx)
}

func (m *Module) attribute(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return 0 // Silently ignore if no context
	}

	// Add security check for accessing OpenTelemetry
	if !security.IsAllowed(l.Context(), "otel.attribute", "", nil) {
		l.RaiseError("not allowed to add attributes to span")
		return 0
	}

	key := l.CheckString(1)
	if key == "" {
		l.ArgError(1, "empty key")
		return 0
	}

	value := l.CheckAny(2)
	if value == nil {
		l.ArgError(2, "nil value")
		return 0
	}

	span := getCurrentSpan(ctx)
	if span == nil {
		return 0 // Silently ignore if no active span
	}

	// Convert Lua value to OpenTelemetry attribute
	var attr attribute.KeyValue
	switch v := value.(type) {
	case lua.LString:
		attr = attribute.String(key, string(v))
	case lua.LNumber:
		attr = attribute.Float64(key, float64(v))
	case lua.LBool:
		attr = attribute.Bool(key, bool(v))
	default:
		l.RaiseError("unsupported attribute type: %T", v)
		return 0
	}

	span.SetAttributes(attr)
	return 0
}

func (m *Module) event(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return 0 // Silently ignore if no context
	}

	// Add security check for accessing OpenTelemetry
	if !security.IsAllowed(l.Context(), "otel.event", "", nil) {
		l.RaiseError("not allowed to add events to span")
		return 0
	}

	name := l.CheckString(1)
	if name == "" {
		l.ArgError(1, "empty event name")
		return 0
	}

	span := getCurrentSpan(ctx)
	if span == nil {
		return 0 // Silently ignore if no active span
	}

	// Get attributes table if provided
	var attrs []attribute.KeyValue
	if l.GetTop() > 1 && l.Get(2).Type() == lua.LTTable {
		tbl := l.Get(2).(*lua.LTable)
		tbl.ForEach(func(k, v lua.LValue) {
			key, ok := k.(lua.LString)
			if !ok {
				return
			}

			switch val := v.(type) {
			case lua.LString:
				attrs = append(attrs, attribute.String(string(key), string(val)))
			case lua.LNumber:
				attrs = append(attrs, attribute.Float64(string(key), float64(val)))
			case lua.LBool:
				attrs = append(attrs, attribute.Bool(string(key), bool(val)))
			}
		})
	}

	span.AddEvent(name, trace.WithAttributes(attrs...))
	return 0
}

func (m *Module) status(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return 0 // Silently ignore if no context
	}

	// Add security check for accessing OpenTelemetry
	if !security.IsAllowed(l.Context(), "otel.status", "", nil) {
		l.RaiseError("not allowed to set span status")
		return 0
	}

	code := l.CheckInt(1)
	message := l.OptString(2, "")

	span := getCurrentSpan(ctx)
	if span == nil {
		return 0 // Silently ignore if no active span
	}

	// Set span status based on code
	// 1 = OK, 2 = Error
	switch code {
	case 1:
		span.SetStatus(codes.Ok, message)
	case 2:
		span.SetStatus(codes.Error, message)
	default:
		l.ArgError(1, "invalid status code: must be 1 (OK) or 2 (Error)")
		return 0
	}

	return 0
}
