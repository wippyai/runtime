// SPDX-License-Identifier: MPL-2.0
package tty

import "github.com/wippyai/go-lua/types/typ"

var imageInfoType = typ.NewRecord().ReadonlyField("id", typ.String).ReadonlyField("format", typ.String).ReadonlyField("width", typ.Integer).ReadonlyField("height", typ.Integer).ReadonlyField("bytes", typ.Integer).Build()
var imageType = typ.NewInterface("tty.Image", []typ.Method{
	{Name: "info", Type: typ.Func().Param("self", typ.Self).Returns(imageInfoType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "read", Type: typ.Func().Param("self", typ.Self).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "close", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
})
var cropType = typ.NewRecord().Field("x", typ.Integer).Field("y", typ.Integer).Field("width", typ.Integer).Field("height", typ.Integer).Build()
var placedImageType = typ.NewRecord().Field("placement_id", typ.String).Field("image", imageType).Field("x", typ.Integer).Field("y", typ.Integer).Field("cols", typ.Integer).Field("rows", typ.Integer).OptField("src", cropType).OptField("z", typ.Integer).OptField("alt", typ.String).Build()
var snapshotImageType = typ.NewRecord().ReadonlyField("placement_id", typ.String).ReadonlyField("image_id", typ.String).ReadonlyField("kind", typ.String).ReadonlyField("x", typ.Integer).ReadonlyField("y", typ.Integer).ReadonlyField("cols", typ.Integer).ReadonlyField("rows", typ.Integer).ReadonlyField("src", cropType).ReadonlyField("z", typ.Integer).ReadonlyField("alt", typ.String).ReadonlyField("resource", imageInfoType).Build()
var captureType = typ.NewInterface("tty.Capture", []typ.Method{
	{Name: "snapshot", Type: typ.Func().Param("self", typ.Self).Returns(viewportSnapshotType).Build()},
	{Name: "image", Type: typ.Func().Param("self", typ.Self).Param("id", typ.String).Returns(imageType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "close", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
})
