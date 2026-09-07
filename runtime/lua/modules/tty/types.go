// SPDX-License-Identifier: MPL-2.0

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
	{Name: "padding", Type: typ.Func().Param("self", typ.Self).Variadic(typ.Integer).Returns(typ.Self).Build()},
	{Name: "margin", Type: typ.Func().Param("self", typ.Self).Variadic(typ.Integer).Returns(typ.Self).Build()},
	{Name: "border", Type: typ.Func().Param("self", typ.Self).Param("name", typ.String).Variadic(typ.Boolean).Returns(typ.Self).Build()},
	{Name: "border_foreground", Type: typ.Func().Param("self", typ.Self).Variadic(typ.String).Returns(typ.Self).Build()},
	{Name: "border_background", Type: typ.Func().Param("self", typ.Self).Variadic(typ.String).Returns(typ.Self).Build()},
	{Name: "width", Type: typ.Func().Param("self", typ.Self).Param("n", typ.Integer).Returns(typ.Self).Build()},
	{Name: "height", Type: typ.Func().Param("self", typ.Self).Param("n", typ.Integer).Returns(typ.Self).Build()},
	{Name: "max_width", Type: typ.Func().Param("self", typ.Self).Param("n", typ.Integer).Returns(typ.Self).Build()},
	{Name: "max_height", Type: typ.Func().Param("self", typ.Self).Param("n", typ.Integer).Returns(typ.Self).Build()},
	{Name: "align", Type: typ.Func().Param("self", typ.Self).Param("pos", typ.Number).Returns(typ.Self).Build()},
	{Name: "align_vertical", Type: typ.Func().Param("self", typ.Self).Param("pos", typ.Number).Returns(typ.Self).Build()},
	{Name: "inline", Type: typ.Func().Param("self", typ.Self).OptParam("enable", typ.Boolean).Returns(typ.Self).Build()},
	{Name: "copy", Type: typ.Func().Param("self", typ.Self).Returns(typ.Self).Build()},
})

// KeyBinding interface returned by tty.bind()
var keyBindingType = typ.NewInterface("tty.KeyBinding", []typ.Method{
	{Name: "matches", Type: typ.Func().Param("self", typ.Self).Param("event", inputEventType).Returns(typ.Boolean).Build()},
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
	ReadonlyField("x", typ.Integer).
	ReadonlyField("y", typ.Integer).
	ReadonlyField("alt", typ.Boolean).
	ReadonlyField("ctrl", typ.Boolean).
	ReadonlyField("shift", typ.Boolean).
	Build()

var resizeEventType = typ.NewRecord().
	ReadonlyField("type", typ.LiteralString("resize")).
	ReadonlyField("width", typ.Integer).
	ReadonlyField("height", typ.Integer).
	Build()

var startEventType = typ.NewRecord().
	ReadonlyField("type", typ.LiteralString("start")).
	ReadonlyField("width", typ.Integer).
	ReadonlyField("height", typ.Integer).
	Build()

var focusEventType = typ.NewRecord().
	ReadonlyField("type", typ.LiteralString("focus")).
	ReadonlyField("focused", typ.Boolean).
	Build()

var visibilityEventType = typ.NewRecord().
	ReadonlyField("type", typ.LiteralString("visibility")).
	ReadonlyField("visible", typ.Boolean).
	Build()

var pasteEventType = typ.NewRecord().
	ReadonlyField("type", typ.LiteralString("paste")).
	ReadonlyField("text", typ.String).
	Build()

var closeEventType = typ.NewRecord().
	ReadonlyField("type", typ.LiteralString("close")).
	Build()

var ttyEventType = typ.NewUnion(
	keyEventType,
	mouseEventType,
	resizeEventType,
	startEventType,
	focusEventType,
	visibilityEventType,
	pasteEventType,
	closeEventType,
)

// Input events accept omitted modifier flags, matching DecodeEvent. Events
// returned by tty.events retain their fully populated output contract.
var inputEventType = typ.NewUnion(
	typ.NewRecord().
		ReadonlyField("type", typ.LiteralString("key")).
		ReadonlyField("key", typ.String).
		ReadonlyField("key_type", typ.String).
		ReadonlyField("action", typ.NewUnion(typ.LiteralString("press"), typ.LiteralString("release"))).
		OptReadonlyField("alt", typ.Boolean).
		OptReadonlyField("ctrl", typ.Boolean).
		OptReadonlyField("shift", typ.Boolean).Build(),
	typ.NewRecord().
		ReadonlyField("type", typ.LiteralString("mouse")).
		ReadonlyField("action", typ.NewUnion(typ.LiteralString("press"), typ.LiteralString("release"), typ.LiteralString("motion"), typ.LiteralString("wheel"))).
		ReadonlyField("button", typ.String).
		ReadonlyField("x", typ.Integer).
		ReadonlyField("y", typ.Integer).
		OptReadonlyField("alt", typ.Boolean).
		OptReadonlyField("ctrl", typ.Boolean).
		OptReadonlyField("shift", typ.Boolean).Build(),
	resizeEventType, startEventType, focusEventType, visibilityEventType, pasteEventType, closeEventType,
)

// InputEventType exposes the event contract accepted by terminal input adapters.
func InputEventType() typ.Type { return inputEventType }

// EventType exposes the structural terminal event contract to optional Lua
// adapters without making them duplicate or weaken it to any.
func EventType() typ.Type { return ttyEventType }

// Channel type for tty.events()
var eventChannelType = typ.NewInterface("tty.EventChannel", []typ.Method{
	{Name: "receive", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(ttyEventType), typ.Boolean).Build()},
	{Name: "case_receive", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any).Build()},
	{Name: "close", Type: typ.Func().Param("self", typ.Self).Build()},
})

var viewportUpdateChannelType = typ.NewInterface("tty.ViewportUpdateChannel", []typ.Method{
	{Name: "receive", Type: typ.Func().Param("self", typ.Self).Returns(typ.Integer, typ.Boolean).Build()},
	{Name: "case_receive", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any).Build()},
})

var surfaceOptionsType = typ.NewRecord().
	OptField("alternate_screen", typ.Boolean).
	OptField("hide_cursor", typ.Boolean).
	OptField("synchronized_output", typ.Boolean).
	Build()

var surfaceStatsType = typ.NewRecord().
	ReadonlyField("rows", typ.Integer).
	ReadonlyField("changed_rows", typ.Integer).
	ReadonlyField("bytes_written", typ.Integer).
	Build()

var surfaceType = typ.NewInterface("tty.Surface", []typ.Method{
	{Name: "present", Type: typ.Func().Param("self", typ.Self).
		Param("rows", typ.NewArray(typ.String)).
		OptParam("options", typ.NewRecord().
			OptField("cursor", typ.NewRecord().
				Field("x", typ.Integer).
				Field("y", typ.Integer).
				Field("visible", typ.Boolean).
				Build()).
			Build()).
		Returns(surfaceStatsType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "invalidate", Type: typ.Func().Param("self", typ.Self).
		Returns(typ.Boolean).Build()},
	{Name: "close", Type: typ.Func().Param("self", typ.Self).
		Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
})

var canvasType = typ.NewInterface("tty.Canvas", []typ.Method{
	{Name: "clear", Type: typ.Func().Param("self", typ.Self).OptParam("fill", typ.String).Returns(typ.Boolean).Build()},
	{Name: "put", Type: typ.Func().Param("self", typ.Self).Param("x", typ.Integer).Param("y", typ.Integer).
		Param("text", typ.String).OptParam("width", typ.Integer).Returns(typ.Boolean).Build()},
	{Name: "put_rows", Type: typ.Func().Param("self", typ.Self).Param("x", typ.Integer).Param("y", typ.Integer).
		Param("rows", typ.NewArray(typ.String)).OptParam("width", typ.Integer).Returns(typ.Boolean).Build()},
	{Name: "rows", Type: typ.Func().Param("self", typ.Self).Returns(typ.NewArray(typ.String)).Build()},
})

var viewportPageType = typ.NewRecord().
	Field("foreground", typ.String).
	Field("background", typ.String).
	Build()

var viewportOptionsType = typ.NewRecord().
	OptField("width", typ.Integer).
	OptField("height", typ.Integer).
	OptField("page", viewportPageType).
	Build()

var viewportSnapshotType = typ.NewRecord().
	ReadonlyField("revision", typ.Integer).
	ReadonlyField("width", typ.Integer).
	ReadonlyField("height", typ.Integer).
	ReadonlyField("rows", typ.NewArray(typ.String)).
	OptReadonlyField("cursor", typ.NewRecord().
		ReadonlyField("x", typ.Integer).
		ReadonlyField("y", typ.Integer).
		ReadonlyField("visible", typ.Boolean).
		Build()).
	Build()

var mountRightsType = typ.NewRecord().
	OptField("observe", typ.Boolean).
	OptField("input", typ.Boolean).
	OptField("resize", typ.Boolean).Build()

var viewportType = typ.NewInterface("tty.Viewport", []typ.Method{
	{Name: "set_page", Type: typ.Func().Param("self", typ.Self).OptParam("page", typ.NewOptional(viewportPageType)).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "mount", Type: typ.Func().Param("self", typ.Self).Param("recipient", typ.String).Param("rights", mountRightsType).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "revoke", Type: typ.Func().Param("self", typ.Self).Param("reference", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "grant", Type: typ.Func().Param("self", typ.Self).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "handle", Type: typ.Func().Param("self", typ.Self).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "snapshot", Type: typ.Func().Param("self", typ.Self).OptParam("after_revision", typ.Integer).Returns(typ.NewOptional(viewportSnapshotType), typ.NewOptional(typ.LuaError)).Build()},
	{Name: "updates", Type: typ.Func().Param("self", typ.Self).Returns(viewportUpdateChannelType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "send", Type: typ.Func().Param("self", typ.Self).Param("event", inputEventType).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "resize", Type: typ.Func().Param("self", typ.Self).Param("width", typ.Integer).Param("height", typ.Integer).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "close", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
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
		ReadonlyField("width", typ.Func().Param("s", typ.String).Returns(typ.Integer).Build()).
		ReadonlyField("truncate", typ.Func().Param("s", typ.String).Param("width", typ.Integer).OptParam("tail", typ.String).Returns(typ.String).Build()).
		ReadonlyField("cut", typ.Func().Param("s", typ.String).Param("left", typ.Integer).Param("right", typ.Integer).Returns(typ.String).Build()).
		ReadonlyField("height", typ.Func().Param("s", typ.String).Returns(typ.Integer).Build()).
		ReadonlyField("size", typ.Func().Param("s", typ.String).Returns(typ.Integer, typ.Integer).Build()).
		ReadonlyField("join_horizontal", typ.Func().Param("pos", typ.Number).Variadic(typ.String).Returns(typ.String).Build()).
		ReadonlyField("join_vertical", typ.Func().Param("pos", typ.Number).Variadic(typ.String).Returns(typ.String).Build()).
		ReadonlyField("max_width", typ.Func().Param("items", typ.NewArray(typ.String)).Returns(typ.Integer).Build()).
		ReadonlyField("max_height", typ.Func().Param("items", typ.NewArray(typ.String)).Returns(typ.Integer).Build()).
		ReadonlyField("place", typ.Func().Param("width", typ.Integer).Param("height", typ.Integer).Param("hPos", typ.Number).Param("vPos", typ.Number).Param("str", typ.String).Returns(typ.String).Build()).
		ReadonlyField("place_horizontal", typ.Func().Param("width", typ.Integer).Param("pos", typ.Number).Param("str", typ.String).Returns(typ.String).Build()).
		ReadonlyField("place_vertical", typ.Func().Param("height", typ.Integer).Param("pos", typ.Number).Param("str", typ.String).Returns(typ.String).Build()).
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
	m.DefineType("VisibilityEvent", visibilityEventType)
	m.DefineType("PasteEvent", pasteEventType)
	m.DefineType("CloseEvent", closeEventType)
	m.DefineType("TTYEvent", ttyEventType)
	m.DefineType("InputEvent", inputEventType)
	m.DefineType("MountRights", mountRightsType)
	m.DefineType("EventChannel", eventChannelType)
	m.DefineType("Surface", surfaceType)
	m.DefineType("Canvas", canvasType)
	m.DefineType("SurfaceOptions", surfaceOptionsType)
	m.DefineType("SurfaceStats", surfaceStatsType)
	m.DefineType("Viewport", viewportType)
	m.DefineType("ViewportOptions", viewportOptionsType)
	m.DefineType("ViewportPage", viewportPageType)
	m.DefineType("ViewportSnapshot", viewportSnapshotType)

	moduleMethodsType := typ.NewInterface("tty", []typ.Method{
		{Name: "start", Type: typ.Func().Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "stop", Type: typ.Func().Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "screen_size", Type: typ.Func().Returns(typ.Integer, typ.Integer, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "events", Type: typ.Func().Returns(eventChannelType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "mouse", Type: typ.Func().Param("enable", typ.Boolean).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "surface", Type: typ.Func().OptParam("options", surfaceOptionsType).Returns(surfaceType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "canvas", Type: typ.Func().Param("width", typ.Integer).Param("height", typ.Integer).Returns(canvasType).Build()},
		{Name: "viewport", Type: typ.Func().OptParam("options", viewportOptionsType).Returns(viewportType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "attach", Type: typ.Func().Param("handle", typ.String).Returns(viewportType, typ.NewOptional(typ.LuaError)).Build()},
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
