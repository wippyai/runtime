package hash

import (
	"github.com/yuin/gopher-lua/types"
)

// ModuleTypes returns the type manifest for the hash module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("hash")

	// hash function type: (data: string, raw?: boolean): string, Error?
	hashFn := types.NewFunction(
		[]types.Type{types.String, types.Optional(types.Boolean)},
		[]types.Type{types.String, types.Optional(types.LuaError)},
	)

	// fnv hash returns number: (data: string): number, Error?
	fnvFn := types.NewFunction(
		[]types.Type{types.String},
		[]types.Type{types.Number, types.Optional(types.LuaError)},
	)

	// hmac function type: (data: string, secret: string, raw?: boolean): string, Error?
	hmacFn := types.NewFunction(
		[]types.Type{types.String, types.String, types.Optional(types.Boolean)},
		[]types.Type{types.String, types.Optional(types.LuaError)},
	)

	moduleType := &types.InterfaceType{
		Name: "hash",
		Methods: map[string]*types.FunctionType{
			"md5":         hashFn,
			"sha1":        hashFn,
			"sha256":      hashFn,
			"sha512":      hashFn,
			"fnv32":       fnvFn,
			"fnv64":       fnvFn,
			"hmac_sha256": hmacFn,
			"hmac_sha512": hmacFn,
			"hmac_sha1":   hmacFn,
			"hmac_md5":    hmacFn,
		},
	}

	m.SetExport(moduleType)
	return m
}
