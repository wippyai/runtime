// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"github.com/wippyai/go-lua/types/contract"
	"github.com/wippyai/go-lua/types/effect"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

// SelectCase type for channel.select
var selectCaseType = typ.NewInterface("channel.SelectCase", nil)

var selectCaseChannel = typ.NewTypeParam("C", nil)
var selectCaseValue = typ.NewTypeParam("T", nil)
var selectCaseGeneric = typ.NewGeneric("channel.SelectCase", []*typ.TypeParam{selectCaseChannel, selectCaseValue}, selectCaseType)

var channelElem = typ.NewTypeParam("T", nil)

// Channel type (generic over element type).
var channelType = typ.NewInterface("channel.Channel", []typ.Method{
	{
		Name: "send",
		Type: typ.Func().
			Param("self", typ.Self).
			Param("value", channelElem).
			Returns(typ.Boolean).
			Spec(contract.NewSpec().WithEffects(effect.Mutate{
				Target:    effect.ParamRef{Index: 0},
				Transform: effect.ContainerElementUnion{Container: effect.ParamRef{Index: 0}, Value: effect.ParamRef{Index: 1}},
			})).
			Build(),
	},
	{
		Name: "receive",
		Type: typ.Func().
			Param("self", typ.Self).
			Returns(channelElem, typ.Boolean).
			Spec(contract.NewSpec().WithEffects(effect.Return{
				ReturnIndex: 0,
				Transform:   effect.ElementOf{Source: effect.ParamRef{Index: 0}},
			})).
			Build(),
	},
	{
		Name: "case_send",
		Type: typ.Func().Param("self", typ.Self).Param("value", channelElem).
			Returns(typ.Instantiate(selectCaseGeneric, typ.Self, channelElem)).
			Build(),
	},
	{
		Name: "case_receive",
		Type: typ.Func().Param("self", typ.Self).
			Returns(typ.Instantiate(selectCaseGeneric, typ.Self, channelElem)).
			Build(),
	},
	{
		Name: "close",
		Type: typ.Func().Param("self", typ.Self).Build(),
	},
})

var channelGeneric = typ.NewGeneric("channel.Channel", []*typ.TypeParam{channelElem}, channelType)

// SelectResult type returned by channel.select
var selectResultType = typ.NewRecord().
	Field("channel", typ.Any).
	Field("value", typ.Unknown).
	Field("ok", typ.Boolean).
	OptField("default", typ.Boolean).
	Build()

// ChannelModuleTypes returns the type manifest for the channel module.
func ChannelModuleTypes() *io.Manifest {
	m := io.NewManifest("channel")

	m.DefineType("Channel", channelGeneric)
	m.DefineType("SelectCase", selectCaseGeneric)
	m.DefineType("SelectResult", selectResultType)

	channelEmpty := typ.Instantiate(channelGeneric, typ.Unknown)
	moduleType := typ.NewInterface("channel", []typ.Method{
		{
			Name: "new",
			Type: typ.Func().OptParam("size", typ.Number).Returns(channelEmpty).Build(),
		},
		{
			Name: "select",
			Type: typ.Func().Param("cases", typ.Any).
				OptParam("default", typ.Boolean).
				Returns(selectResultType).
				Spec(contract.NewSpec().WithEffects(effect.Return{
					ReturnIndex: 0,
					Transform: effect.SelectResultOfCases{
						Cases:   effect.ParamRef{Index: 0},
						Default: effect.ParamRef{Index: 1},
					},
				})).
				Build(),
		},
	})

	m.Export = moduleType
	return m
}
