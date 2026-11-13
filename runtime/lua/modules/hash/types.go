// SPDX-License-Identifier: MPL-2.0

package hash

import (
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// ModuleTypes returns the type manifest for the hash module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("hash")

	// hash function type: (data: string, raw?: boolean): string, Error?
	hashFn := typ.Func().Param("data", typ.String).OptParam("raw", typ.Boolean).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()

	// fnv hash returns number: (data: string): number, Error?
	fnvFn := typ.Func().Param("data", typ.String).Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()

	// hmac function type: (data: string, secret: string, raw?: boolean): string, Error?
	hmacFn := typ.Func().Param("data", typ.String).Param("secret", typ.String).OptParam("raw", typ.Boolean).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()

	// pbkdf2 function type: (password: string, salt: string, iterations: number, key_length: number, algo?: string): string, Error?
	pbkdf2Fn := typ.Func().Param("password", typ.String).Param("salt", typ.String).Param("iterations", typ.Number).Param("key_length", typ.Number).OptParam("algo", typ.String).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()

	// streaming hasher: hash.new(algorithm: string): Hasher, Error?
	hasherType := typ.NewInterface("hash.Hasher", []typ.Method{
		{Name: "update", Type: typ.Func().Param("self", typ.Self).Param("data", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "sum", Type: typ.Func().Param("self", typ.Self).OptParam("raw", typ.Boolean).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "reset", Type: typ.Func().Param("self", typ.Self).Build()},
	})
	m.DefineType("Hasher", hasherType)
	newFn := typ.Func().Param("algorithm", typ.String).Returns(hasherType, typ.NewOptional(typ.LuaError)).Build()

	minhashType := minhashTypes(m)

	methodsType := typ.NewInterface("hash", []typ.Method{
		{Name: "md5", Type: hashFn},
		{Name: "sha1", Type: hashFn},
		{Name: "sha256", Type: hashFn},
		{Name: "sha512", Type: hashFn},
		{Name: "fnv32", Type: fnvFn},
		{Name: "fnv64", Type: fnvFn},
		{Name: "hmac_sha256", Type: hmacFn},
		{Name: "hmac_sha512", Type: hmacFn},
		{Name: "hmac_sha1", Type: hmacFn},
		{Name: "hmac_md5", Type: hmacFn},
		{Name: "pbkdf2", Type: pbkdf2Fn},
		{Name: "new", Type: newFn},
	})

	fieldsType := typ.NewRecord().Field("minhash", minhashType).Build()

	m.SetExport(typ.NewIntersection(methodsType, fieldsType))
	return m
}

// minhashTypes defines the MinHash types and answers the hash.minhash namespace.
func minhashTypes(m *io.Manifest) typ.Type {
	input := typ.NewUnion(typ.String, typ.NewArray(typ.String))
	sketchOptions := typ.NewRecord().
		OptField("k", typ.Integer).
		OptField("shingle", typ.Integer).
		Build()
	bandOptions := typ.NewRecord().
		OptField("bands", typ.Integer).
		OptField("rows", typ.Integer).
		OptField("domain", typ.String).
		Build()
	options := typ.NewRecord().
		OptField("k", typ.Integer).
		OptField("shingle", typ.Integer).
		OptField("bands", typ.Integer).
		OptField("rows", typ.Integer).
		OptField("domain", typ.String).
		Build()
	signature := typ.NewRecord().
		Field("tokens", typ.Integer).
		Field("normalized", typ.String).
		OptField("sketch", typ.String).
		OptField("bands", typ.NewArray(typ.String)).
		Build()
	hasher := typ.NewInterface("hash.MinHasher", []typ.Method{
		{Name: "update", Type: typ.Func().Param("self", typ.Self).Param("chunk", typ.String).Returns(typ.Boolean, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "sum", Type: typ.Func().Param("self", typ.Self).Returns(signature, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "reset", Type: typ.Func().Param("self", typ.Self).Build()},
	})

	m.DefineType("MinHashOptions", options)
	m.DefineType("MinHashSketchOptions", sketchOptions)
	m.DefineType("MinHashBandOptions", bandOptions)
	m.DefineType("MinHashSignature", signature)
	m.DefineType("MinHasher", hasher)

	return typ.NewInterface("hash.minhash", []typ.Method{
		{Name: "signature", Type: typ.Func().Param("input", input).OptParam("options", options).Returns(signature, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "sketch", Type: typ.Func().Param("input", input).OptParam("options", sketchOptions).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "bands", Type: typ.Func().Param("sketch", typ.String).OptParam("options", bandOptions).Returns(typ.NewArray(typ.String), typ.NewOptional(typ.LuaError)).Build()},
		{Name: "similarity", Type: typ.Func().Param("a", typ.String).Param("b", typ.String).Returns(typ.Number, typ.NewOptional(typ.LuaError)).Build()},
		{Name: "new", Type: typ.Func().OptParam("options", options).Returns(hasher, typ.NewOptional(typ.LuaError)).Build()},
	})
}
