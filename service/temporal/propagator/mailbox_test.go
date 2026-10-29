// SPDX-License-Identifier: MPL-2.0

package propagator

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"google.golang.org/protobuf/proto"
)

func TestContinuedMailboxPreservesEnvelope(t *testing.T) {
	dc := newTestDataConverter()
	messages := []process.MailboxEnvelope{{Source: pid.PID{Host: "sender", UniqID: "one"}, Topic: "data", Payloads: payload.Payloads{payload.NewPayload([]byte(`"value"`), payload.JSON)}}}
	header, err := AddMailboxToHeader(dc, nil, messages)
	require.NoError(t, err)
	restored, err := ExtractMailboxFromHeader(dc, header)
	require.NoError(t, err)
	require.Equal(t, messages[0].Topic, restored[0].Topic)
	require.Equal(t, messages[0].Source.String(), restored[0].Source.String())
	wire, err := dc.ToPayload(restored[0].Payloads[0])
	require.NoError(t, err)
	var value string
	require.NoError(t, dc.FromPayload(wire, &value))
	require.Equal(t, "value", value)
}

func TestContinuedMailboxPreservesPayloadKindsDeterministically(t *testing.T) {
	dc := newTestDataConverter()
	messages := []process.MailboxEnvelope{{Topic: "future", Payloads: payload.Payloads{payload.NewPayload([]byte{0, 1, 2}, payload.Bytes), payload.New(nil), payload.NewTerminal()}}}
	first, err := AddMailboxToHeader(dc, nil, messages)
	require.NoError(t, err)
	second, err := AddMailboxToHeader(dc, nil, messages)
	require.NoError(t, err)
	require.True(t, proto.Equal(first, second))
	restored, err := ExtractMailboxFromHeader(dc, first)
	require.NoError(t, err)
	require.Len(t, restored, 1)
	require.Len(t, restored[0].Payloads, 3)
	require.Equal(t, []byte{0, 1, 2}, restored[0].Payloads[0].Data())
	require.Nil(t, restored[0].Payloads[1].Data())
	require.True(t, payload.IsTerminal(restored[0].Payloads[2]))
}
