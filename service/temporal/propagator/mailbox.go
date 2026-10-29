// SPDX-License-Identifier: MPL-2.0

package propagator

import (
	"fmt"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	"google.golang.org/protobuf/proto"
)

const mailboxHeaderKey = "wippy-upgrade-mailbox"

// AddMailboxToHeader encodes the canonical unread envelopes with the execution's
// converter. Continuation headers come from the worker, like workflow input.
func AddMailboxToHeader(dc converter.DataConverter, header *commonpb.Header, messages []process.MailboxEnvelope) (*commonpb.Header, error) {
	if len(messages) == 0 {
		return header, nil
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
	value := &commonpb.Payload{Data: data}
	if header == nil {
		header = &commonpb.Header{}
	}
	if header.Fields == nil {
		header.Fields = make(map[string]*commonpb.Payload)
	}
	header.Fields[mailboxHeaderKey] = value
	return header, nil
}

// ExtractMailboxFromHeader decodes unread envelopes from a continuation.
// New signals continue to pass through their independent relay-ticket verifier.
func ExtractMailboxFromHeader(dc converter.DataConverter, header *commonpb.Header) ([]process.MailboxEnvelope, error) {
	if header == nil {
		return nil, nil
	}
	value, present := header.Fields[mailboxHeaderKey]
	if !present {
		return nil, nil
	}
	if value == nil {
		return nil, fmt.Errorf("invalid continued mailbox")
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
