// SPDX-License-Identifier: MPL-2.0

package propagator

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	commonpb "go.temporal.io/api/common/v1"
	"google.golang.org/protobuf/proto"
)

func TestContinuedMailboxPreservesEnvelope(t *testing.T) {
	dc := newTestDataConverter()
	messages := []process.MailboxEnvelope{{Source: pid.PID{Host: "sender", UniqID: "one"}, Topic: "data", Payloads: payload.Payloads{payload.NewPayload([]byte(`"value"`), payload.JSON)}}}
	header, err := AddMailboxToHeader(dc, nil, messages, "workflow", "previous-run")
	require.NoError(t, err)
	restored, err := ExtractMailboxFromHeader(dc, header, "workflow", "previous-run")
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
	first, err := AddMailboxToHeader(dc, nil, messages, "workflow", "previous-run")
	require.NoError(t, err)
	second, err := AddMailboxToHeader(dc, nil, messages, "workflow", "previous-run")
	require.NoError(t, err)
	require.True(t, proto.Equal(first, second))
	restored, err := ExtractMailboxFromHeader(dc, first, "workflow", "previous-run")
	require.NoError(t, err)
	require.Len(t, restored, 1)
	require.Len(t, restored[0].Payloads, 3)
	require.Equal(t, []byte{0, 1, 2}, restored[0].Payloads[0].Data())
	require.Nil(t, restored[0].Payloads[1].Data())
	require.True(t, payload.IsTerminal(restored[0].Payloads[2]))
}

func TestContinuedMailboxAuthenticatesContentAndExecution(t *testing.T) {
	dc := newTestDataConverter()
	key := []byte("0123456789abcdef0123456789abcdef")
	previousKey := []byte("abcdef0123456789abcdef0123456789")
	messages := []process.MailboxEnvelope{{Topic: "data", Payloads: payload.Payloads{payload.NewPayload([]byte(`"accepted"`), payload.JSON)}}}
	header, err := AddMailboxToHeader(dc, nil, messages, "workflow", "previous-run", key)
	require.NoError(t, err)
	for _, test := range []struct {
		name                  string
		mutate                func(*commonpb.Header)
		workflow, previousRun string
		wantError             string
		keys                  [][]byte
	}{
		{name: "valid", workflow: "workflow", previousRun: "previous-run", keys: [][]byte{key}},
		{name: "rotation", workflow: "workflow", previousRun: "previous-run", keys: [][]byte{previousKey, key}},
		{name: "other-workflow", workflow: "other", previousRun: "previous-run", keys: [][]byte{key}, wantError: "execution"},
		{name: "other-run", workflow: "workflow", previousRun: "other-run", keys: [][]byte{key}, wantError: "execution"},
		{name: "initial-start", workflow: "workflow", keys: [][]byte{key}, wantError: "execution"},
		{name: "wrong-key", workflow: "workflow", previousRun: "previous-run", keys: [][]byte{previousKey}, wantError: "signature"},
		{name: "unsigned", workflow: "workflow", previousRun: "previous-run", keys: [][]byte{key}, mutate: func(h *commonpb.Header) { delete(h.Fields, mailboxSignatureHeaderKey) }, wantError: "signature"},
		{name: "orphan-signature", workflow: "workflow", previousRun: "previous-run", keys: [][]byte{key}, mutate: func(h *commonpb.Header) { delete(h.Fields, mailboxHeaderKey) }, wantError: "invalid continued mailbox"},
		{name: "changed-content", workflow: "workflow", previousRun: "previous-run", keys: [][]byte{key}, mutate: func(h *commonpb.Header) { h.Fields[mailboxHeaderKey].Data = []byte("forged") }, wantError: "signature"},
		{name: "changed-audience", workflow: "other", previousRun: "previous-run", keys: [][]byte{key}, mutate: func(h *commonpb.Header) { h.Fields[mailboxHeaderKey].Metadata["workflow"] = []byte("other") }, wantError: "signature"},
		{name: "short-key", workflow: "workflow", previousRun: "previous-run", keys: [][]byte{[]byte("short")}, wantError: "32 bytes"},
		{name: "missing-key", workflow: "workflow", previousRun: "previous-run", wantError: "32 bytes"},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := proto.Clone(header).(*commonpb.Header)
			if test.mutate != nil {
				test.mutate(candidate)
			}
			restored, err := ExtractMailboxFromHeader(dc, candidate, test.workflow, test.previousRun, test.keys...)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				require.Nil(t, restored)
				return
			}
			require.NoError(t, err)
			require.Len(t, restored, 1)
			wire, err := dc.ToPayload(restored[0].Payloads[0])
			require.NoError(t, err)
			var value string
			require.NoError(t, dc.FromPayload(wire, &value))
			require.Equal(t, "accepted", value)
		})
	}
}
