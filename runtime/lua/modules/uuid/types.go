package uuid

import "github.com/yuin/gopher-lua/types"

// UUIDInfo type returned by parse
var uuidInfoType = &types.RecordType{
	Name: "uuid.Info",
	Fields: []types.RecordField{
		{Name: "version", Type: types.Number},
		{Name: "variant", Type: types.String},
		{Name: "timestamp", Type: types.Number, Optional: true},
		{Name: "node", Type: types.String, Optional: true},
	},
}

// ModuleTypes returns the type manifest for the uuid module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("uuid")

	m.DefineType("Info", uuidInfoType)

	// Generator functions: (): string, Error?
	generatorFn := types.NewFunction(
		nil,
		[]types.Type{types.String, types.Optional(types.LuaError)},
	)

	// v3/v5 functions: (namespace: string, name: string): string, Error?
	namespacedFn := types.NewFunction(
		[]types.Type{types.String, types.String},
		[]types.Type{types.String, types.Optional(types.LuaError)},
	)

	moduleType := &types.InterfaceType{
		Name: "uuid",
		Methods: map[string]*types.FunctionType{
			// uuid.v1(): string, Error?
			"v1": generatorFn,
			// uuid.v3(namespace, name): string, Error?
			"v3": namespacedFn,
			// uuid.v4(): string, Error?
			"v4": generatorFn,
			// uuid.v5(namespace, name): string, Error?
			"v5": namespacedFn,
			// uuid.v7(): string, Error?
			"v7": generatorFn,
			// uuid.validate(id: string): boolean, Error?
			"validate": types.NewFunction(
				[]types.Type{types.String},
				[]types.Type{types.Boolean, types.Optional(types.LuaError)},
			),
			// uuid.version(id: string): number, Error?
			"version": types.NewFunction(
				[]types.Type{types.String},
				[]types.Type{types.Number, types.Optional(types.LuaError)},
			),
			// uuid.variant(id: string): string, Error?
			"variant": types.NewFunction(
				[]types.Type{types.String},
				[]types.Type{types.String, types.Optional(types.LuaError)},
			),
			// uuid.parse(id: string): Info, Error?
			"parse": types.NewFunction(
				[]types.Type{types.String},
				[]types.Type{uuidInfoType, types.Optional(types.LuaError)},
			),
			// uuid.format(id: string, format?: string): string, Error?
			"format": types.NewFunction(
				[]types.Type{types.String, types.Optional(types.String)},
				[]types.Type{types.String, types.Optional(types.LuaError)},
			),
		},
	}

	m.SetExport(moduleType)
	return m
}
