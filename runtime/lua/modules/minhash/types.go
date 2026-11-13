// SPDX-License-Identifier: MPL-2.0

package minhash

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// ModuleTypes returns the type manifest for the minhash module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("minhash")

	sketchOptions := typ.NewRecord().
		OptField("shingle", typ.Integer).
		OptField("k", typ.Integer).
		OptField("segment", typ.Integer).
		OptField("modulus", typ.Integer).
		OptField("base", typ.Integer).
		OptField("probe", typ.Integer).
		OptField("stride", typ.Integer).
		Build()
	bandOptions := typ.NewRecord().
		OptField("bands", typ.Integer).
		OptField("rows", typ.Integer).
		OptField("domain", typ.String).
		Build()
	signatureOptions := typ.NewRecord().
		OptField("shingle", typ.Integer).
		OptField("k", typ.Integer).
		OptField("segment", typ.Integer).
		OptField("modulus", typ.Integer).
		OptField("base", typ.Integer).
		OptField("probe", typ.Integer).
		OptField("stride", typ.Integer).
		OptField("bands", typ.Integer).
		OptField("rows", typ.Integer).
		OptField("domain", typ.String).
		Build()
	signature := typ.NewRecord().
		Field("words", typ.Integer).
		Field("normalized_sha256", typ.String).
		OptField("sketch", typ.String).
		OptField("bands", typ.NewArray(typ.String)).
		Build()

	m.DefineType("SketchOptions", sketchOptions)
	m.DefineType("BandOptions", bandOptions)
	m.DefineType("SignatureOptions", signatureOptions)
	m.DefineType("Signature", signature)

	sketchFn := typ.Func().Param("text", typ.String).OptParam("options", sketchOptions).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()
	signatureFn := typ.Func().Param("text", typ.String).OptParam("options", signatureOptions).Returns(signature, typ.NewOptional(typ.LuaError)).Build()
	bandsFn := typ.Func().Param("sketch", typ.String).OptParam("options", bandOptions).Returns(typ.NewArray(typ.String), typ.NewOptional(typ.LuaError)).Build()
	estimateFn := typ.Func().Param("a", typ.String).Param("b", typ.String).Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()

	m.SetExport(typ.NewInterface("minhash", []typ.Method{
		{Name: "sketch", Type: sketchFn},
		{Name: "signature", Type: signatureFn},
		{Name: "bands", Type: bandsFn},
		{Name: "estimate", Type: estimateFn},
	}))
	return m
}
