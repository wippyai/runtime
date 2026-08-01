// SPDX-License-Identifier: MPL-2.0

package propagator

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"

	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
)

const (
	RelaySignalHeaderKey          = "wippy-relay-signal"
	RelaySignalSignatureHeaderKey = "wippy-relay-signal-signature"
	RelaySignalOperation          = "signal"
)

// RelaySignalTicket attests that the runtime already authorized delivery of a
// relay signal. It is separate from the actor/scope claim used to initialize
// workflow execution authority.
type RelaySignalTicket struct {
	Audience  string `json:"audience"`
	Operation string `json:"operation"`
	Signal    string `json:"signal"`
}

type relaySignalContextKey struct{}

// WithRelaySignal marks a Temporal call as an authorized relay-signal
// delivery. The Temporal propagator owns and applies the signing key.
func WithRelaySignal(ctx context.Context, workflowID, signal string) context.Context {
	return context.WithValue(ctx, relaySignalContextKey{}, RelaySignalTicket{
		Audience:  workflowID,
		Operation: RelaySignalOperation,
		Signal:    signal,
	})
}

func relaySignalFromContext(ctx context.Context) (RelaySignalTicket, bool) {
	ticket, ok := ctx.Value(relaySignalContextKey{}).(RelaySignalTicket)
	return ticket, ok
}

func relaySignalMAC(key []byte, payload *commonpb.Payload) []byte {
	return temporalPayloadMAC("wippy/temporal/relay-signal/v1", key, payload)
}

// AddRelaySignalToHeader signs a relay-signal ticket and adds it to header.
func AddRelaySignalToHeader(
	dc converter.DataConverter,
	header *commonpb.Header,
	ticket RelaySignalTicket,
	keys ...[]byte,
) (*commonpb.Header, error) {
	if dc == nil {
		return header, fmt.Errorf("data converter not available")
	}
	if ticket.Audience == "" || ticket.Operation != RelaySignalOperation || ticket.Signal == "" {
		return header, fmt.Errorf("invalid temporal relay signal ticket")
	}
	resolvedKeys, err := resolveSecurityKeys(keys)
	if err != nil {
		return header, err
	}
	payload, err := encodeSecurityEnvelope(ticket)
	if err != nil {
		return header, err
	}
	if header == nil {
		header = &commonpb.Header{Fields: make(map[string]*commonpb.Payload)}
	}
	if header.Fields == nil {
		header.Fields = make(map[string]*commonpb.Payload)
	}
	header.Fields[RelaySignalHeaderKey] = payload
	header.Fields[RelaySignalSignatureHeaderKey] = &commonpb.Payload{Data: relaySignalMAC(resolvedKeys[0], payload)}
	return header, nil
}

// ExtractRelaySignalTicket verifies a relay-signal ticket for the exact target
// workflow and signal. An absent ticket returns nil without error.
func ExtractRelaySignalTicket(
	dc converter.DataConverter,
	header *commonpb.Header,
	expectedAudience string,
	expectedSignal string,
	keys ...[]byte,
) (*RelaySignalTicket, error) {
	if dc == nil {
		return nil, fmt.Errorf("data converter not available")
	}
	if header == nil || header.Fields == nil {
		return nil, nil
	}
	payload, hasPayload := header.Fields[RelaySignalHeaderKey]
	signature, hasSignature := header.Fields[RelaySignalSignatureHeaderKey]
	if !hasPayload && !hasSignature {
		return nil, nil
	}
	if !hasPayload || payload == nil || !hasSignature || signature == nil {
		return nil, fmt.Errorf("incomplete temporal relay signal header")
	}
	resolvedKeys, err := resolveSecurityKeys(keys)
	if err != nil {
		return nil, err
	}
	validSignature := false
	if len(signature.Data) == sha256.Size {
		for _, key := range resolvedKeys {
			if hmac.Equal(signature.Data, relaySignalMAC(key, payload)) {
				validSignature = true
				break
			}
		}
	}
	if !validSignature {
		return nil, fmt.Errorf("invalid temporal relay signal signature")
	}

	var ticket RelaySignalTicket
	if err := decodeSecurityEnvelope(payload, &ticket); err != nil {
		return nil, err
	}
	if expectedAudience == "" || ticket.Audience != expectedAudience ||
		ticket.Operation != RelaySignalOperation || expectedSignal == "" || ticket.Signal != expectedSignal {
		return nil, fmt.Errorf("invalid temporal relay signal target")
	}
	return &ticket, nil
}
