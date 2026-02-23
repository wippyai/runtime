// SPDX-License-Identifier: MPL-2.0

package propagator

import (
	"github.com/wippyai/runtime/service/temporal/dataconverter"
	syspayload "github.com/wippyai/runtime/system/payload"
	jsonpayload "github.com/wippyai/runtime/system/payload/json"
	msgpayload "github.com/wippyai/runtime/system/payload/msgpack"
	"go.temporal.io/sdk/converter"
)

func newTestDataConverter() converter.DataConverter {
	dtt := syspayload.NewTranscoder()
	jsonpayload.Register(dtt)
	msgpayload.Register(dtt)
	return dataconverter.NewDataConverter(dtt)
}
