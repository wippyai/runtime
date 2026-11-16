// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	"github.com/wippyai/runtime/runtime/lua/modules/fs"
	"github.com/wippyai/runtime/runtime/lua/modules/stream"
)

var stringMap = typ.NewMap(typ.String, typ.String)

var elementType = typ.NewRecursive("xml.Element", func(self typ.Type) typ.Type {
	return typ.NewRecord().
		Field("name", typ.String).
		OptField("ns", typ.String).
		OptField("attrs", stringMap).
		OptField("xmlns", stringMap).
		OptField("text", typ.String).
		OptField("children", typ.NewArray(typ.NewUnion(typ.String, self))).
		Build()
})

var readOptionsType = typ.NewRecord().
	OptField("whitespace", typ.NewUnion(typ.LiteralString("preserve"), typ.LiteralString("strip"))).
	OptField("comments", typ.Boolean).
	OptField("instructions", typ.Boolean).
	OptField("max_depth", typ.Integer).
	OptField("max_attrs", typ.Integer).
	OptField("max_namespaces", typ.Integer).
	OptField("max_tag_bytes", typ.Integer).
	OptField("max_token_bytes", typ.Integer).
	OptField("max_node_bytes", typ.Integer).
	OptField("max_bytes", typ.Integer).
	Build()

var writeOptionsType = typ.NewRecord().
	OptField("indent", typ.String).
	OptField("declaration", typ.Boolean).
	OptField("ns", stringMap).
	OptField("max_depth", typ.Integer).
	OptField("max_attrs", typ.Integer).
	OptField("max_namespaces", typ.Integer).
	OptField("max_node_bytes", typ.Integer).
	OptField("max_tag_bytes", typ.Integer).
	Build()

var textOptionsType = typ.NewRecord().
	OptField("max_node_bytes", typ.Integer).
	OptField("max_depth", typ.Integer).
	Build()

var findOptionsType = typ.NewRecord().
	OptField("ns", stringMap).
	OptField("max_node_bytes", typ.Integer).
	OptField("max_depth", typ.Integer).
	Build()

var selectOptionsType = typ.NewRecord().
	OptField("ns", stringMap).
	Build()

var eventKindType = typ.NewUnion(
	typ.LiteralString("start"),
	typ.LiteralString("end"),
	typ.LiteralString("text"),
	typ.LiteralString("comment"),
	typ.LiteralString("pi"),
)

var positionType = typ.NewRecord().
	Field("line", typ.Integer).
	Field("column", typ.Integer).
	Field("offset", typ.Integer).
	Build()

var optErr = typ.NewOptional(typ.LuaError)

func method(name string, fn *typ.Function) typ.Method { return typ.Method{Name: name, Type: fn} }

var recordsType = typ.NewInterface("xml.Records", []typ.Method{
	method("read", typ.Func().Param("self", typ.Self).OptParam("n", typ.Integer).
		Returns(typ.NewOptional(typ.NewArray(elementType)), optErr).Build()),
	method("close", typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()),
})

var writerType = typ.NewInterface("xml.Writer", []typ.Method{
	method("start_element", typ.Func().Param("self", typ.Self).Param("name", typ.String).
		OptParam("attrs", stringMap).OptParam("ns", typ.String).Returns(typ.Boolean, optErr).Build()),
	method("end_element", typ.Func().Param("self", typ.Self).Returns(typ.Boolean, optErr).Build()),
	method("text", typ.Func().Param("self", typ.Self).Param("data", typ.String).Returns(typ.Boolean, optErr).Build()),
	method("element", typ.Func().Param("self", typ.Self).Param("name", typ.String).
		OptParam("attrs", stringMap).OptParam("text", typ.String).OptParam("ns", typ.String).
		Returns(typ.Boolean, optErr).Build()),
	method("node", typ.Func().Param("self", typ.Self).Param("element", elementType).Returns(typ.Boolean, optErr).Build()),
	method("comment", typ.Func().Param("self", typ.Self).Param("data", typ.String).Returns(typ.Boolean, optErr).Build()),
	method("flush", typ.Func().Param("self", typ.Self).Returns(typ.Boolean, optErr).Build()),
	method("finish", typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(typ.String), optErr).Build()),
	method("close", typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()),
})

var readerType = typ.NewInterface("xml.Reader", []typ.Method{
	method("select", typ.Func().Param("self", typ.Self).Param("paths", typ.NewUnion(typ.String, typ.NewArray(typ.String))).
		OptParam("options", selectOptionsType).Returns(recordsType, optErr).Build()),
	method("next", typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(eventKindType), optErr).Build()),
	method("kind", typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(eventKindType)).Build()),
	method("name", typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(typ.String)).Build()),
	method("ns", typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(typ.String)).Build()),
	method("attr", typ.Func().Param("self", typ.Self).Param("name", typ.String).OptParam("ns", typ.String).
		Returns(typ.NewOptional(typ.String)).Build()),
	method("attrs", typ.Func().Param("self", typ.Self).Returns(stringMap).Build()),
	method("text", typ.Func().Param("self", typ.Self).Returns(typ.NewOptional(typ.String)).Build()),
	method("depth", typ.Func().Param("self", typ.Self).Returns(typ.Integer).Build()),
	method("pos", typ.Func().Param("self", typ.Self).Returns(positionType).Build()),
	method("node", typ.Func().Param("self", typ.Self).Returns(elementType, optErr).Build()),
	method("skip", typ.Func().Param("self", typ.Self).Returns(typ.Boolean, optErr).Build()),
	method("copy", typ.Func().Param("self", typ.Self).Param("writer", writerType).Returns(typ.Boolean, optErr).Build()),
	method("close", typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()),
})

func lookupType(m *io.Manifest, name string) typ.Type {
	if m != nil {
		if t, ok := m.LookupType(name); ok {
			return t
		}
	}
	return typ.Any
}

// ModuleTypes returns the type manifest for the xml module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("xml")
	source := typ.NewUnion(lookupType(stream.ModuleTypes(), "Stream"), lookupType(fs.ModuleTypes(), "File"))

	m.DefineType("Element", elementType)
	m.DefineType("ReadOptions", readOptionsType)
	m.DefineType("WriteOptions", writeOptionsType)
	m.DefineType("SelectOptions", selectOptionsType)
	m.DefineType("TextOptions", textOptionsType)
	m.DefineType("FindOptions", findOptionsType)
	m.DefineType("EventKind", eventKindType)
	m.DefineType("Position", positionType)
	m.DefineType("Reader", readerType)
	m.DefineType("Records", recordsType)
	m.DefineType("Writer", writerType)

	m.SetExport(typ.NewInterface("xml", []typ.Method{
		method("decode", typ.Func().Param("data", typ.String).OptParam("options", readOptionsType).Returns(elementType, optErr).Build()),
		method("encode", typ.Func().Param("element", elementType).OptParam("options", writeOptionsType).Returns(typ.String, optErr).Build()),
		method("text", typ.Func().Param("element", elementType).OptParam("options", textOptionsType).Returns(typ.String).Build()),
		method("attr", typ.Func().Param("element", elementType).Param("name", typ.String).OptParam("ns", typ.String).
			Returns(typ.NewOptional(typ.String)).Build()),
		method("child", typ.Func().Param("element", elementType).Param("name", typ.String).OptParam("ns", typ.String).
			Returns(typ.NewOptional(elementType)).Build()),
		method("children", typ.Func().Param("element", elementType).OptParam("name", typ.String).OptParam("ns", typ.String).
			Returns(typ.NewArray(elementType)).Build()),
		method("find", typ.Func().Param("element", elementType).Param("path", typ.String).OptParam("options", findOptionsType).
			Returns(typ.NewOptional(typ.NewArray(elementType)), optErr).Build()),
		method("resolve", typ.Func().Param("element", elementType).Param("qname", typ.String).
			Returns(typ.String, typ.NewOptional(typ.String), optErr).Build()),
		method("reader", typ.Func().Param("source", typ.NewUnion(typ.String, source)).OptParam("options", readOptionsType).
			Returns(readerType, optErr).Build()),
		method("writer", typ.Func().OptParam("dest", source).OptParam("options", writeOptionsType).
			Returns(writerType, optErr).Build()),
	}))
	return m
}
