package tty

import (
	typio "github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// Style interface returned by tty.style()
var styleType = typ.NewInterface("tty.Style", []typ.Method{
	{Name: "render", Type: typ.Func().Param("self", typ.Self).Variadic(typ.String).Returns(typ.String).Build()},
	{Name: "foreground", Type: typ.Func().Param("self", typ.Self).Param("color", typ.String).Returns(typ.Self).Build()},
	{Name: "background", Type: typ.Func().Param("self", typ.Self).Param("color", typ.String).Returns(typ.Self).Build()},
	{Name: "bold", Type: typ.Func().Param("self", typ.Self).OptParam("enable", typ.Boolean).Returns(typ.Self).Build()},
	{Name: "italic", Type: typ.Func().Param("self", typ.Self).OptParam("enable", typ.Boolean).Returns(typ.Self).Build()},
	{Name: "underline", Type: typ.Func().Param("self", typ.Self).OptParam("enable", typ.Boolean).Returns(typ.Self).Build()},
	{Name: "strikethrough", Type: typ.Func().Param("self", typ.Self).OptParam("enable", typ.Boolean).Returns(typ.Self).Build()},
	{Name: "faint", Type: typ.Func().Param("self", typ.Self).OptParam("enable", typ.Boolean).Returns(typ.Self).Build()},
	{Name: "blink", Type: typ.Func().Param("self", typ.Self).OptParam("enable", typ.Boolean).Returns(typ.Self).Build()},
	{Name: "reverse", Type: typ.Func().Param("self", typ.Self).OptParam("enable", typ.Boolean).Returns(typ.Self).Build()},
	{Name: "padding", Type: typ.Func().Param("self", typ.Self).Variadic(typ.Number).Returns(typ.Self).Build()},
	{Name: "margin", Type: typ.Func().Param("self", typ.Self).Variadic(typ.Number).Returns(typ.Self).Build()},
	{Name: "border", Type: typ.Func().Param("self", typ.Self).Param("name", typ.String).Variadic(typ.Boolean).Returns(typ.Self).Build()},
	{Name: "border_foreground", Type: typ.Func().Param("self", typ.Self).Variadic(typ.String).Returns(typ.Self).Build()},
	{Name: "border_background", Type: typ.Func().Param("self", typ.Self).Variadic(typ.String).Returns(typ.Self).Build()},
	{Name: "width", Type: typ.Func().Param("self", typ.Self).Param("n", typ.Number).Returns(typ.Self).Build()},
	{Name: "height", Type: typ.Func().Param("self", typ.Self).Param("n", typ.Number).Returns(typ.Self).Build()},
	{Name: "max_width", Type: typ.Func().Param("self", typ.Self).Param("n", typ.Number).Returns(typ.Self).Build()},
	{Name: "max_height", Type: typ.Func().Param("self", typ.Self).Param("n", typ.Number).Returns(typ.Self).Build()},
	{Name: "align", Type: typ.Func().Param("self", typ.Self).Param("pos", typ.Number).Returns(typ.Self).Build()},
	{Name: "align_vertical", Type: typ.Func().Param("self", typ.Self).Param("pos", typ.Number).Returns(typ.Self).Build()},
	{Name: "inline", Type: typ.Func().Param("self", typ.Self).OptParam("enable", typ.Boolean).Returns(typ.Self).Build()},
	{Name: "copy", Type: typ.Func().Param("self", typ.Self).Returns(typ.Self).Build()},
})

// KeyBinding interface returned by tty.bind()
var keyBindingType = typ.NewInterface("tty.KeyBinding", []typ.Method{
	{Name: "matches", Type: typ.Func().Param("self", typ.Self).Param("event", typ.Any).Returns(typ.Boolean).Build()},
	{Name: "set_enabled", Type: typ.Func().Param("self", typ.Self).Param("enabled", typ.Boolean).Returns(typ.Self).Build()},
	{Name: "is_enabled", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
	{Name: "help", Type: typ.Func().Param("self", typ.Self).Returns(bindingHelpType).Build()},
})

var bindingHelpType = typ.NewRecord().
	ReadonlyField("key", typ.String).
	ReadonlyField("desc", typ.String).
	Build()

var bindingConfigType = typ.NewRecord().
	Field("keys", typ.NewArray(typ.String)).
	OptField("help", typ.NewRecord().
		OptField("key", typ.String).
		OptField("desc", typ.String).
		Build()).
	Build()

// Event types as discriminated union on "type" field
var keyEventType = typ.NewRecord().
	ReadonlyField("type", typ.LiteralString("key")).
	ReadonlyField("key", typ.String).
	ReadonlyField("key_type", typ.String).
	ReadonlyField("action", typ.NewUnion(typ.LiteralString("press"), typ.LiteralString("release"))).
	ReadonlyField("alt", typ.Boolean).
	ReadonlyField("ctrl", typ.Boolean).
	ReadonlyField("shift", typ.Boolean).
	Build()

var mouseEventType = typ.NewRecord().
	ReadonlyField("type", typ.LiteralString("mouse")).
	ReadonlyField("action", typ.NewUnion(
		typ.LiteralString("press"),
		typ.LiteralString("release"),
		typ.LiteralString("motion"),
		typ.LiteralString("wheel"),
	)).
	ReadonlyField("button", typ.String).
	ReadonlyField("x", typ.Number).
	ReadonlyField("y", typ.Number).
	ReadonlyField("alt", typ.Boolean).
	ReadonlyField("ctrl", typ.Boolean).
	ReadonlyField("shift", typ.Boolean).
	Build()

var resizeEventType = typ.NewRecord().
	ReadonlyField("type", typ.LiteralString("resize")).
	ReadonlyField("width", typ.Number).
	ReadonlyField("height", typ.Number).
	Build()

var startEventType = typ.NewRecord().
	ReadonlyField("type", typ.LiteralString("start")).
	ReadonlyField("width", typ.Number).
	ReadonlyField("height", typ.Number).
	Build()

var focusEventType = typ.NewRecord().
	ReadonlyField("type", typ.LiteralString("focus")).
	ReadonlyField("focused", typ.Boolean).
	Build()

var pasteEventType = typ.NewRecord().
	ReadonlyField("type", typ.LiteralString("paste")).
	ReadonlyField("text", typ.String).
	Build()

var ttyEventType = typ.NewUnion(
	keyEventType,
	mouseEventType,
	resizeEventType,
	startEventType,
	focusEventType,
	pasteEventType,
)

// Channel type for tty.events()
var eventChannelType = typ.NewInterface("tty.EventChannel", []typ.Method{
	{Name: "receive", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(ttyEventType)).Build()},
})

// Border constants
var bordersConstType = typ.NewRecord().
	ReadonlyField("NORMAL", typ.String).
	ReadonlyField("ROUNDED", typ.String).
	ReadonlyField("THICK", typ.String).
	ReadonlyField("DOUBLE", typ.String).
	ReadonlyField("HIDDEN", typ.String).
	Build()

// Alignment constants
var alignConstType = typ.NewRecord().
	ReadonlyField("LEFT", typ.Number).
	ReadonlyField("CENTER", typ.Number).
	ReadonlyField("RIGHT", typ.Number).
	Build()

// Position constants
var positionConstType = typ.NewRecord().
	ReadonlyField("TOP", typ.Number).
	ReadonlyField("LEFT", typ.Number).
	ReadonlyField("CENTER", typ.Number).
	ReadonlyField("BOTTOM", typ.Number).
	ReadonlyField("RIGHT", typ.Number).
	Build()

// Text sub-module
var textModType typ.Type

func init() {
	textModType = typ.NewRecord().
		ReadonlyField("width", typ.Func().Param("s", typ.String).Returns(typ.Number).Build()).
		ReadonlyField("height", typ.Func().Param("s", typ.String).Returns(typ.Number).Build()).
		ReadonlyField("size", typ.Func().Param("s", typ.String).Returns(typ.Number, typ.Number).Build()).
		ReadonlyField("join_horizontal", typ.Func().Param("pos", typ.Number).Variadic(typ.String).Returns(typ.String).Build()).
		ReadonlyField("join_vertical", typ.Func().Param("pos", typ.Number).Variadic(typ.String).Returns(typ.String).Build()).
		ReadonlyField("max_width", typ.Func().Param("items", typ.NewArray(typ.String)).Returns(typ.Number).Build()).
		ReadonlyField("max_height", typ.Func().Param("items", typ.NewArray(typ.String)).Returns(typ.Number).Build()).
		ReadonlyField("place", typ.Func().Param("width", typ.Number).Param("height", typ.Number).Param("hPos", typ.Number).Param("vPos", typ.Number).Param("str", typ.String).Returns(typ.String).Build()).
		ReadonlyField("place_horizontal", typ.Func().Param("width", typ.Number).Param("pos", typ.Number).Param("str", typ.String).Returns(typ.String).Build()).
		ReadonlyField("place_vertical", typ.Func().Param("height", typ.Number).Param("pos", typ.Number).Param("str", typ.String).Returns(typ.String).Build()).
		ReadonlyField("position", positionConstType).
		Build()
}

// ModuleTypes returns the type manifest for the tty module.
func ModuleTypes() *typio.Manifest {
	m := typio.NewManifest("tty")

	m.DefineType("Style", styleType)
	m.DefineType("KeyBinding", keyBindingType)
	m.DefineType("BindingHelp", bindingHelpType)
	m.DefineType("BindingConfig", bindingConfigType)
	m.DefineType("KeyEvent", keyEventType)
	m.DefineType("MouseEvent", mouseEventType)
	m.DefineType("ResizeEvent", resizeEventType)
	m.DefineType("StartEvent", startEventType)
	m.DefineType("FocusEvent", focusEventType)
	m.DefineType("PasteEvent", pasteEventType)
	m.DefineType("TTYEvent", ttyEventType)
	m.DefineType("EventChannel", eventChannelType)

	moduleMethodsType := typ.NewInterface("tty", []typ.Method{
		{Name: "start", Type: typ.Func().Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "stop", Type: typ.Func().Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "screen_size", Type: typ.Func().Returns(typ.Number, typ.Number, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "events", Type: typ.Func().Returns(eventChannelType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "mouse", Type: typ.Func().Param("enable", typ.Boolean).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "style", Type: typ.Func().Returns(styleType).Build()},
		{Name: "bind", Type: typ.Func().Param("config", bindingConfigType).Returns(keyBindingType).Build()},
	})

	moduleFieldsType := typ.NewRecord().
		ReadonlyField("borders", bordersConstType).
		ReadonlyField("align", alignConstType).
		ReadonlyField("text", textModType).
		Build()

	m.SetExport(typ.NewIntersection(moduleMethodsType, moduleFieldsType))
	return m
}
