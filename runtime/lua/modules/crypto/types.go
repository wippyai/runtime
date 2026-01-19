package crypto

import (
	"github.com/yuin/gopher-lua/types/io"
	"github.com/yuin/gopher-lua/types/typ"
)

// Submodule types for crypto
var randomType = typ.NewInterface("crypto.random", []typ.Method{
	{
		Name: "bytes",
		Type: typ.Func().Param("length", typ.Number).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build(),
	},
	{
		Name: "string",
		Type: typ.Func().Param("length", typ.Number).OptParam("charset", typ.String).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build(),
	},
	{
		Name: "uuid",
		Type: typ.Func().Returns(typ.String, typ.NewOptional(typ.LuaError)).Build(),
	},
})

var hmacType = typ.NewInterface("crypto.hmac", []typ.Method{
	{
		Name: "sha256",
		Type: typ.Func().Param("key", typ.String).Param("data", typ.String).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build(),
	},
	{
		Name: "sha512",
		Type: typ.Func().Param("key", typ.String).Param("data", typ.String).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build(),
	},
})

var encryptType = typ.NewInterface("crypto.encrypt", []typ.Method{
	{
		Name: "aes",
		Type: typ.Func().Param("data", typ.String).Param("key", typ.String).OptParam("aad", typ.String).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build(),
	},
	{
		Name: "chacha20",
		Type: typ.Func().Param("data", typ.String).Param("key", typ.String).OptParam("aad", typ.String).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build(),
	},
})

var decryptType = typ.NewInterface("crypto.decrypt", []typ.Method{
	{
		Name: "aes",
		Type: typ.Func().Param("data", typ.String).Param("key", typ.String).OptParam("aad", typ.String).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build(),
	},
	{
		Name: "chacha20",
		Type: typ.Func().Param("data", typ.String).Param("key", typ.String).OptParam("aad", typ.String).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build(),
	},
})

var jwtType = typ.NewInterface("crypto.jwt", []typ.Method{
	{
		Name: "encode",
		Type: typ.Func().Param("payload", typ.Any).Param("key", typ.String).OptParam("alg", typ.String).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build(),
	},
	{
		Name: "verify",
		Type: typ.Func().Param("token", typ.String).Param("key", typ.String).OptParam("alg", typ.String).OptParam("require_exp", typ.Boolean).Returns(typ.Any, typ.NewOptional(typ.LuaError)).Build(),
	},
})

// cryptoModuleType combines methods and submodule fields
var cryptoModuleMethods = typ.NewInterface("crypto", []typ.Method{
	{
		Name: "pbkdf2",
		Type: typ.Func().Param("password", typ.String).Param("salt", typ.String).Param("iterations", typ.Number).Param("key_length", typ.Number).OptParam("hash", typ.String).Returns(typ.String, typ.NewOptional(typ.LuaError)).Build(),
	},
	{
		Name: "constant_time_compare",
		Type: typ.Func().Param("a", typ.String).Param("b", typ.String).Returns(typ.Boolean).Build(),
	},
})

// ModuleTypes returns the type manifest for the crypto module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("crypto")

	// Export record with submodules as fields
	moduleType := typ.NewRecord().
		Field("random", randomType).
		Field("hmac", hmacType).
		Field("encrypt", encryptType).
		Field("decrypt", decryptType).
		Field("jwt", jwtType).
		Build()

	// Use intersection of methods and fields
	m.SetExport(typ.NewIntersection(cryptoModuleMethods, moduleType))
	return m
}
