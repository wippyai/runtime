// SPDX-License-Identifier: MPL-2.0

package propagator

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	"google.golang.org/protobuf/proto"
)

const (
	mailboxHeaderKey          = "wippy-upgrade-mailbox"
	mailboxSignatureHeaderKey = "wippy-upgrade-mailbox-signature"
)

// AddMailboxToHeader encodes the canonical unread envelopes with the execution's
// converter, bound to its workflow and first run of the execution chain.
// Keyed workers authenticate the complete mailbox independently of actor claims.
func AddMailboxToHeader(dc converter.DataConverter, header *commonpb.Header, messages []process.MailboxEnvelope, workflowID, firstRunID string, keys ...[]byte) (*commonpb.Header, error) {
	if len(messages) == 0 {
		return header, nil
	}
	if dc == nil || workflowID == "" || firstRunID == "" {
		return nil, fmt.Errorf("continued mailbox requires a converter, workflow ID and first run ID")
	}
	var signingKeys [][]byte
	if len(keys) > 0 {
		var err error
		signingKeys, err = resolveSecurityKeys(keys)
		if err != nil {
			return nil, err
		}
	}
	mailbox := &commonpb.Payloads{}
	for _, message := range messages {
		values, err := dc.ToPayloads(message.Payloads)
		if err != nil {
			return nil, err
		}
		data, err := (proto.MarshalOptions{Deterministic: true}).Marshal(values)
		if err != nil {
			return nil, err
		}
		mailbox.Payloads = append(mailbox.Payloads, &commonpb.Payload{
			Metadata: map[string][]byte{"source": []byte(message.Source.String()), "topic": []byte(message.Topic)}, Data: data,
		})
	}
	data, err := (proto.MarshalOptions{Deterministic: true}).Marshal(mailbox)
	if err != nil {
		return nil, err
	}
	value := &commonpb.Payload{Metadata: map[string][]byte{"workflow": []byte(workflowID), "first-run": []byte(firstRunID)}, Data: data}
	if header == nil {
		header = &commonpb.Header{}
	}
	if header.Fields == nil {
		header.Fields = make(map[string]*commonpb.Payload)
	}
	header.Fields[mailboxHeaderKey] = value
	if len(signingKeys) > 0 {
		header.Fields[mailboxSignatureHeaderKey] = &commonpb.Payload{Data: temporalPayloadMAC("wippy/temporal/upgrade-mailbox/v1", signingKeys[0], value)}
	} else {
		delete(header.Fields, mailboxSignatureHeaderKey)
	}
	return header, nil
}

// ExtractMailboxFromHeader decodes unread envelopes from a continuation.
// It verifies continuation identity and, on keyed workers, mailbox authenticity
// before decoding payloads. New signals use their relay-ticket verifier.
func ExtractMailboxFromHeader(dc converter.DataConverter, header *commonpb.Header, workflowID, firstRunID string, keys ...[]byte) ([]process.MailboxEnvelope, error) {
	if header == nil {
		return nil, nil
	}
	value, present := header.Fields[mailboxHeaderKey]
	signature, signed := header.Fields[mailboxSignatureHeaderKey]
	if !present && !signed {
		return nil, nil
	}
	if value == nil || dc == nil {
		return nil, fmt.Errorf("invalid continued mailbox")
	}
	// FirstRunID survives continue-as-new, retry and reset. A new execution
	// reusing the workflow ID receives a different chain identity.
	if workflowID == "" || firstRunID == "" || string(value.Metadata["workflow"]) != workflowID || string(value.Metadata["first-run"]) != firstRunID {
		return nil, fmt.Errorf("invalid continued mailbox execution")
	}
	if len(keys) > 0 || signed {
		if !signed || signature == nil || len(signature.Data) != sha256.Size {
			return nil, fmt.Errorf("continued mailbox requires a valid signature")
		}
		verificationKeys, err := resolveSecurityKeys(keys)
		if err != nil {
			return nil, err
		}
		valid := false
		for _, key := range verificationKeys {
			if hmac.Equal(signature.Data, temporalPayloadMAC("wippy/temporal/upgrade-mailbox/v1", key, value)) {
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("invalid continued mailbox signature")
		}
	}
	mailbox := &commonpb.Payloads{}
	if err := proto.Unmarshal(value.Data, mailbox); err != nil {
		return nil, err
	}
	messages := make([]process.MailboxEnvelope, len(mailbox.Payloads))
	for i, entry := range mailbox.Payloads {
		if entry == nil {
			return nil, fmt.Errorf("invalid continued mailbox entry")
		}
		source, err := pid.ParsePID(string(entry.Metadata["source"]))
		if err != nil {
			return nil, fmt.Errorf("invalid continued mailbox sender: %w", err)
		}
		messages[i].Source, messages[i].Topic = source, string(entry.Metadata["topic"])
		values := &commonpb.Payloads{}
		if err := proto.Unmarshal(entry.Data, values); err != nil {
			return nil, err
		}
		if err := dc.FromPayloads(values, &messages[i].Payloads); err != nil {
			return nil, err
		}
	}
	return messages, nil
}
