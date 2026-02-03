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

	moduleType := typ.NewInterface("hash", []typ.Method{
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
	})

	m.SetExport(moduleType)
	return m
}
