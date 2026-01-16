package engine

import (
	"github.com/yuin/gopher-lua/types"
)

// SelectCase type for channel.select
var selectCaseType = &types.InterfaceType{
	Name: "channel.SelectCase",
}

// Channel type
var channelType = &types.InterfaceType{
	Name: "channel.Channel",
	Methods: map[string]*types.FunctionType{
		"send":         types.NewFunction([]types.Type{types.Self, types.Any}, []types.Type{types.Boolean}),
		"receive":      types.NewFunction([]types.Type{types.Self}, []types.Type{types.Any, types.Boolean}),
		"case_send":    types.NewFunction([]types.Type{types.Self, types.Any}, []types.Type{selectCaseType}),
		"case_receive": types.NewFunction([]types.Type{types.Self}, []types.Type{selectCaseType}),
		"close":        types.NewFunction([]types.Type{types.Self}, nil),
	},
}

// SelectResult type returned by channel.select
var selectResultType = &types.RecordType{
	Name: "channel.SelectResult",
	Fields: []types.RecordField{
		{Name: "channel", Type: types.Any},
		{Name: "value", Type: types.Any},
		{Name: "ok", Type: types.Boolean},
		{Name: "default", Type: types.Optional(types.Boolean)},
	},
}

// ChannelModuleTypes returns the type manifest for the channel module.
func ChannelModuleTypes() *types.TypeManifest {
	m := types.NewManifest("channel")

	m.DefineType("Channel", channelType)
	m.DefineType("SelectResult", selectResultType)

	moduleType := &types.InterfaceType{
		Name: "channel",
		Methods: map[string]*types.FunctionType{
			"new":    types.NewFunction([]types.Type{types.Optional(types.Number)}, []types.Type{channelType}),
			"select": types.NewFunction([]types.Type{types.Any}, []types.Type{selectResultType}),
		},
	}

	m.SetExport(moduleType)
	return m
}
