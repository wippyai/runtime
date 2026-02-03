package html

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// Forward declarations for mutually referential types
var (
	policyType      typ.Type
	attrBuilderType typ.Type
	sanitizeType    typ.Type
)

func init() {
	// AttrBuilder methods
	attrBuilderMethods := []typ.Method{
		{Name: "on_elements", Type: typ.Func().Param("self", typ.Self).Variadic(typ.String).Returns(typ.Self).Build()},
		{Name: "globally", Type: typ.Func().Param("self", typ.Self).Returns(typ.Self).Build()},
		{Name: "matching", Type: typ.Func().Param("self", typ.Self).Param("pattern", typ.String).Returns(typ.Self, typ.NewOptional(typ.LuaError)).Build()},
	}
	attrBuilderType = typ.NewInterface("html.AttrBuilder", attrBuilderMethods)

	// Policy methods (some return Policy, some reference AttrBuilder)
	policyMethods := []typ.Method{
		{Name: "allow_elements", Type: typ.Func().Param("self", typ.Self).Variadic(typ.String).Returns(typ.Self).Build()},
		{Name: "allow_attrs", Type: typ.Func().Param("self", typ.Self).Variadic(typ.String).Returns(attrBuilderType).Build()},
		{Name: "allow_standard_urls", Type: typ.Func().Param("self", typ.Self).Returns(typ.Self).Build()},
		{Name: "require_parseable_urls", Type: typ.Func().Param("self", typ.Self).Param("required", typ.Boolean).Returns(typ.Self).Build()},
		{Name: "allow_relative_urls", Type: typ.Func().Param("self", typ.Self).Param("allowed", typ.Boolean).Returns(typ.Self).Build()},
		{Name: "allow_url_schemes", Type: typ.Func().Param("self", typ.Self).Variadic(typ.String).Returns(typ.Self).Build()},
		{Name: "require_nofollow_on_links", Type: typ.Func().Param("self", typ.Self).Param("required", typ.Boolean).Returns(typ.Self).Build()},
		{Name: "require_noreferrer_on_links", Type: typ.Func().Param("self", typ.Self).Param("required", typ.Boolean).Returns(typ.Self).Build()},
		{Name: "add_target_blank_to_fully_qualified_links", Type: typ.Func().Param("self", typ.Self).Param("add", typ.Boolean).Returns(typ.Self).Build()},
		{Name: "allow_data_uri_images", Type: typ.Func().Param("self", typ.Self).Returns(typ.Self).Build()},
		{Name: "allow_standard_attributes", Type: typ.Func().Param("self", typ.Self).Returns(typ.Self).Build()},
		{Name: "allow_images", Type: typ.Func().Param("self", typ.Self).Returns(typ.Self).Build()},
		{Name: "allow_lists", Type: typ.Func().Param("self", typ.Self).Returns(typ.Self).Build()},
		{Name: "allow_tables", Type: typ.Func().Param("self", typ.Self).Returns(typ.Self).Build()},
		{Name: "sanitize", Type: typ.Func().Param("self", typ.Self).Param("html", typ.String).Returns(typ.String).Build()},
	}
	policyType = typ.NewInterface("html.Policy", policyMethods)

	// sanitize submodule type - must be initialized here after policyType is set
	sanitizeMethods := []typ.Method{
		{Name: "new_policy", Type: typ.Func().Returns(policyType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "ugc_policy", Type: typ.Func().Returns(policyType, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "strict_policy", Type: typ.Func().Returns(policyType, typ.NewOptional(typ.LuaError)).Build()},
	}
	sanitizeType = typ.NewInterface("html.sanitize", sanitizeMethods)
}

// ModuleTypes returns the type manifest for the html module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("html")

	m.DefineType("Policy", policyType)
	m.DefineType("AttrBuilder", attrBuilderType)

	// Module exports: sanitize is a submodule accessed as html.sanitize
	moduleType := typ.NewRecord().
		Field("sanitize", sanitizeType).
		Build()

	m.SetExport(moduleType)
	return m
}
