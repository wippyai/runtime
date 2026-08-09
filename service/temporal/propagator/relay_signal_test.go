// SPDX-License-Identifier: MPL-2.0

package propagator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	commonpb "go.temporal.io/api/common/v1"
)

func TestRelaySignalTicket(t *testing.T) {
	dc := newTestDataConverter()
	ticket := RelaySignalTicket{
		Audience:  "workflow-1",
		Operation: RelaySignalOperation,
		Signal:    "message",
	}

	header, err := AddRelaySignalToHeader(dc, nil, ticket, testSecurityHMACKey)
	require.NoError(t, err)
	extracted, err := ExtractRelaySignalTicket(dc, header, "workflow-1", "message", testSecurityHMACKey)
	require.NoError(t, err)
	require.Equal(t, ticket, *extracted)

	_, err = ExtractRelaySignalTicket(dc, header, "workflow-2", "message", testSecurityHMACKey)
	require.ErrorContains(t, err, "target")
	_, err = ExtractRelaySignalTicket(dc, header, "workflow-1", "other", testSecurityHMACKey)
	require.ErrorContains(t, err, "target")
}

func TestRelaySignalTicketRejectsInvalidClaims(t *testing.T) {
	dc := newTestDataConverter()
	for _, ticket := range []RelaySignalTicket{
		{Operation: RelaySignalOperation, Signal: "message"},
		{Audience: "workflow-1", Operation: "query", Signal: "message"},
		{Audience: "workflow-1", Operation: RelaySignalOperation},
	} {
		_, err := AddRelaySignalToHeader(dc, nil, ticket, testSecurityHMACKey)
		require.Error(t, err)
	}

	payload, err := dc.ToPayload(RelaySignalTicket{
		Audience:  "workflow-1",
		Operation: "query",
		Signal:    "message",
	})
	require.NoError(t, err)
	header := &commonpb.Header{Fields: map[string]*commonpb.Payload{
		RelaySignalHeaderKey:          payload,
		RelaySignalSignatureHeaderKey: {Data: relaySignalMAC(testSecurityHMACKey, payload)},
	}}
	_, err = ExtractRelaySignalTicket(dc, header, "workflow-1", "message", testSecurityHMACKey)
	require.ErrorContains(t, err, "target")
}

func TestRelaySignalTicketRejectsTamperingAndSignatureConfusion(t *testing.T) {
	dc := newTestDataConverter()
	ticket := RelaySignalTicket{Audience: "workflow-1", Operation: RelaySignalOperation, Signal: "message"}
	header, err := AddRelaySignalToHeader(dc, nil, ticket, testSecurityHMACKey)
	require.NoError(t, err)
	header.Fields[RelaySignalHeaderKey].Data = append(header.Fields[RelaySignalHeaderKey].Data, 'x')
	_, err = ExtractRelaySignalTicket(dc, header, "workflow-1", "message", testSecurityHMACKey)
	require.ErrorContains(t, err, "signature")

	securityHeader, err := AddSecurityToHeader(dc, nil, &SecurityPayload{
		Actor:    &ActorPayload{ID: "user"},
		Audience: "workflow-1",
		Scope:    true,
	}, testSecurityHMACKey)
	require.NoError(t, err)
	confused := &commonpb.Header{Fields: map[string]*commonpb.Payload{
		RelaySignalHeaderKey:          securityHeader.Fields[SecurityHeaderKey],
		RelaySignalSignatureHeaderKey: securityHeader.Fields[SecuritySignatureHeaderKey],
	}}
	_, err = ExtractRelaySignalTicket(dc, confused, "workflow-1", "message", testSecurityHMACKey)
	require.ErrorContains(t, err, "signature")
}

func TestRelaySignalTicketSupportsKeyRotation(t *testing.T) {
	dc := newTestDataConverter()
	previousKey := []byte("previous-0123456789abcdef01234567")
	activeKey := []byte("active---0123456789abcdef01234567")
	ticket := RelaySignalTicket{Audience: "workflow-1", Operation: RelaySignalOperation, Signal: "message"}
	header, err := AddRelaySignalToHeader(dc, nil, ticket, previousKey)
	require.NoError(t, err)
	_, err = ExtractRelaySignalTicket(dc, header, "workflow-1", "message", activeKey, previousKey)
	require.NoError(t, err)
}

func TestPropagatorInjectRelaySignal(t *testing.T) {
	dc := newTestDataConverter()
	ctx := WithRelaySignal(context.Background(), "workflow-1", "message")

	writer := &mockHeaderWriter{fields: make(map[string]*commonpb.Payload)}
	require.NoError(t, New(dc, testSecurityHMACKey).Inject(ctx, writer))
	header := &commonpb.Header{Fields: writer.fields}
	_, err := ExtractRelaySignalTicket(dc, header, "workflow-1", "message", testSecurityHMACKey)
	require.NoError(t, err)

	writer = &mockHeaderWriter{fields: make(map[string]*commonpb.Payload)}
	require.NoError(t, New(dc).Inject(ctx, writer))
	require.NotContains(t, writer.fields, RelaySignalHeaderKey)
}
