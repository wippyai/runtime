package crypto

import "github.com/yuin/gopher-lua/types"

// Submodule types for crypto
var randomType = &types.InterfaceType{
	Name: "crypto.random",
	Methods: map[string]*types.FunctionType{
		// random.bytes(length: integer): string, Error?
		"bytes": types.NewFunction(
			[]types.Type{types.Number},
			[]types.Type{types.String, types.Optional(types.LuaError)},
		),
		// random.string(length: integer, charset?: string): string, Error?
		"string": types.NewFunction(
			[]types.Type{types.Number, types.Optional(types.String)},
			[]types.Type{types.String, types.Optional(types.LuaError)},
		),
		// random.uuid(): string, Error?
		"uuid": types.NewFunction(
			nil,
			[]types.Type{types.String, types.Optional(types.LuaError)},
		),
	},
}

var hmacType = &types.InterfaceType{
	Name: "crypto.hmac",
	Methods: map[string]*types.FunctionType{
		// hmac.sha256(key: string, data: string): string, Error?
		"sha256": types.NewFunction(
			[]types.Type{types.String, types.String},
			[]types.Type{types.String, types.Optional(types.LuaError)},
		),
		// hmac.sha512(key: string, data: string): string, Error?
		"sha512": types.NewFunction(
			[]types.Type{types.String, types.String},
			[]types.Type{types.String, types.Optional(types.LuaError)},
		),
	},
}

var encryptType = &types.InterfaceType{
	Name: "crypto.encrypt",
	Methods: map[string]*types.FunctionType{
		// encrypt.aes(data: string, key: string, aad?: string): string, Error?
		"aes": types.NewFunction(
			[]types.Type{types.String, types.String, types.Optional(types.String)},
			[]types.Type{types.String, types.Optional(types.LuaError)},
		),
		// encrypt.chacha20(data: string, key: string, aad?: string): string, Error?
		"chacha20": types.NewFunction(
			[]types.Type{types.String, types.String, types.Optional(types.String)},
			[]types.Type{types.String, types.Optional(types.LuaError)},
		),
	},
}

var decryptType = &types.InterfaceType{
	Name: "crypto.decrypt",
	Methods: map[string]*types.FunctionType{
		// decrypt.aes(data: string, key: string, aad?: string): string, Error?
		"aes": types.NewFunction(
			[]types.Type{types.String, types.String, types.Optional(types.String)},
			[]types.Type{types.String, types.Optional(types.LuaError)},
		),
		// decrypt.chacha20(data: string, key: string, aad?: string): string, Error?
		"chacha20": types.NewFunction(
			[]types.Type{types.String, types.String, types.Optional(types.String)},
			[]types.Type{types.String, types.Optional(types.LuaError)},
		),
	},
}

var jwtType = &types.InterfaceType{
	Name: "crypto.jwt",
	Methods: map[string]*types.FunctionType{
		// jwt.encode(payload: table, key: string, alg?: string): string, Error?
		"encode": types.NewFunction(
			[]types.Type{types.Any, types.String, types.Optional(types.String)},
			[]types.Type{types.String, types.Optional(types.LuaError)},
		),
		// jwt.verify(token: string, key: string, alg?: string, require_exp?: boolean): table, Error?
		"verify": types.NewFunction(
			[]types.Type{types.String, types.String, types.Optional(types.String), types.Optional(types.Boolean)},
			[]types.Type{types.Any, types.Optional(types.LuaError)},
		),
	},
}

// ModuleTypes returns the type manifest for the crypto module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("crypto")

	moduleType := &types.InterfaceType{
		Name: "crypto",
		Fields: map[string]types.Type{
			"random":  randomType,
			"hmac":    hmacType,
			"encrypt": encryptType,
			"decrypt": decryptType,
			"jwt":     jwtType,
		},
		Methods: map[string]*types.FunctionType{
			// crypto.pbkdf2(password: string, salt: string, iterations: integer, key_length: integer, hash?: string): string, Error?
			"pbkdf2": types.NewFunction(
				[]types.Type{types.String, types.String, types.Number, types.Number, types.Optional(types.String)},
				[]types.Type{types.String, types.Optional(types.LuaError)},
			),
			// crypto.constant_time_compare(a: string, b: string): boolean
			"constant_time_compare": types.NewFunction(
				[]types.Type{types.String, types.String},
				[]types.Type{types.Boolean},
			),
		},
	}

	m.SetExport(moduleType)
	return m
}
