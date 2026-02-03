package text

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// Regexp userdata type
var regexpType = typ.NewInterface("text.Regexp", []typ.Method{
	{Name: "find_all_string_submatch", Type: typ.Func().Param("self", typ.Self).Param("s", typ.String).Returns(typ.Any).Build()},
	{Name: "find_string_submatch", Type: typ.Func().Param("self", typ.Self).Param("s", typ.String).Returns(typ.Any).Build()},
	{Name: "find_all_string", Type: typ.Func().Param("self", typ.Self).Param("s", typ.String).Returns(typ.Any).Build()},
	{Name: "find_string", Type: typ.Func().Param("self", typ.Self).Param("s", typ.String).Returns(typ.NewOptional(typ.String)).Build()},
	{Name: "find_all_string_index", Type: typ.Func().Param("self", typ.Self).Param("s", typ.String).Returns(typ.Any).Build()},
	{Name: "find_string_index", Type: typ.Func().Param("self", typ.Self).Param("s", typ.String).Returns(typ.Any).Build()},
	{Name: "replace_all_string", Type: typ.Func().Param("self", typ.Self).Param("s", typ.String).Param("repl", typ.String).Returns(typ.String).Build()},
	{Name: "match_string", Type: typ.Func().Param("self", typ.Self).Param("s", typ.String).Returns(typ.Boolean).Build()},
	{Name: "split", Type: typ.Func().Param("self", typ.Self).Param("s", typ.String).OptParam("n", typ.Number).Returns(typ.Any).Build()},
	{Name: "num_subexp", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
	{Name: "subexp_names", Type: typ.Func().Param("self", typ.Self).Returns(typ.Any).Build()},
	{Name: "string", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
})

// DiffResult type
var diffResultType = typ.NewRecord().
	Field("operation", typ.String).
	Field("text", typ.String).
	Build()

// Differ userdata type
var differType = typ.NewInterface("text.Differ", []typ.Method{
	{Name: "compare", Type: typ.Func().Param("self", typ.Self).Param("a", typ.String).Param("b", typ.String).Returns(typ.NewArray(diffResultType), typ.NewOptional(typ.LuaError)).Build()},
	{Name: "pretty_text", Type: typ.Func().Param("self", typ.Self).Param("d", typ.Any).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "pretty_html", Type: typ.Func().Param("self", typ.Self).Param("d", typ.Any).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "patch_make", Type: typ.Func().Param("self", typ.Self).Param("a", typ.String).Param("b", typ.String).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "patch_apply", Type: typ.Func().Param("self", typ.Self).Param("patches", typ.Any).Param("text", typ.String).Returns(typ.String, typ.Boolean).Build()},
	{Name: "summarize", Type: typ.Func().Param("self", typ.Self).Param("diffs", typ.Any).Returns(typ.Any).Build()},
})

// Splitter userdata type
var splitterType = typ.NewInterface("text.Splitter", []typ.Method{
	{Name: "split_text", Type: typ.Func().Param("self", typ.Self).Param("text", typ.String).Returns(typ.NewArray(typ.String), typ.NewOptional(typ.LuaError)).Build()},
	{Name: "split_batch", Type: typ.Func().Param("self", typ.Self).Param("batch", typ.Any).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build()},
})

// Submodule types
var regexpModType = typ.NewInterface("text.regexp", []typ.Method{
	{Name: "compile", Type: typ.Func().Param("pattern", typ.String).Returns(regexpType, typ.NewOptional(typ.LuaError)).Build()},
})

var diffModType = typ.NewInterface("text.diff", []typ.Method{
	{Name: "new", Type: typ.Func().OptParam("options", typ.Any).Returns(differType, typ.NewOptional(typ.LuaError)).Build()},
})

var splitterModType = typ.NewInterface("text.splitter", []typ.Method{
	{Name: "recursive", Type: typ.Func().OptParam("options", typ.Any).Returns(splitterType, typ.NewOptional(typ.LuaError)).Build()},
	{Name: "markdown", Type: typ.Func().OptParam("options", typ.Any).Returns(splitterType, typ.NewOptional(typ.LuaError)).Build()},
})

// ModuleTypes returns the type manifest for the text module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("text")

	m.DefineType("Regexp", regexpType)
	m.DefineType("Differ", differType)
	m.DefineType("Splitter", splitterType)
	m.DefineType("DiffResult", diffResultType)

	// Module has submodules accessed as fields
	moduleType := typ.NewRecord().
		Field("regexp", regexpModType).
		Field("diff", diffModType).
		Field("splitter", splitterModType).
		Build()

	m.SetExport(moduleType)
	return m
}
