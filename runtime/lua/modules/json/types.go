// SPDX-License-Identifier: MPL-2.0

package json

import (
	"github.com/wippyai/go-lua/types/contract"
	"github.com/wippyai/go-lua/types/effect"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// ModuleTypes returns the type manifest for the json module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("json")

	// Schema accepts any table structure or string reference
	schemaParam := typ.NewUnion(typ.Any, typ.String)
	decodeEffects := effect.Row{Labels: []effect.Label{
		effect.Return{ReturnIndex: 0, Transform: effect.TypeValueOf{Source: effect.ParamRef{Index: 1}}},
		effect.ErrorReturn{ValueIndex: 0, ErrorIndex: 1},
	}}

	moduleType := typ.NewInterface("json", []typ.Method{
		{
			Name: "encode",
			Type: typ.Func().Param("value", typ.Any).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build(),
		},
		{
			Name: "decode",
			Type: typ.Func().Effects(decodeEffects).Spec(contract.NewSpec().WithEffectRow(decodeEffects)).
				Param("str", typ.String).OptParam("target", typ.NewMeta(typ.Any)).
				Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build(),
		},
		{
			Name: "validate",
			Type: typ.Func().Param("schema", schemaParam).Param("data", typ.Any).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build(),
		},
		{
			Name: "validate_string",
			Type: typ.Func().Param("schema", schemaParam).Param("str", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build(),
		},
	})

	m.SetExport(moduleType)
	return m
}
