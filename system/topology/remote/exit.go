// SPDX-License-Identifier: MPL-2.0

package remote

import (
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

const ExitTopic = "@topology/exit/1"

// Completion is decoded remote evidence, not authorization to deliver an actor
// EXIT. Monitor names the original installed request. The origin service must
// match the exact grant, request, actors and live connection before delivery.
type Completion struct {
	Monitor Control
	Result  CompletionResult
}

// EncodeCompletion creates one native message with exactly two JSON payloads:
// a bounded versioned identity and the bounded completion result. The caller
// retains ownership until relay admission succeeds.
func EncodeCompletion(completion Completion) (*relay.Package, error) {
	if completion.Monitor.Kind != MonitorControl {
		return nil, ErrInvalidControl
	}
	data, err := EncodeResult(completion.Result)
	if err != nil {
		return nil, err
	}
	identity := completion.Monitor
	identity.Kind = exitedControl
	pkg, err := encodeControl(identity)
	if err != nil {
		return nil, err
	}
	pkg.Messages[0].Topic = ExitTopic
	pkg.Messages[0].Payloads = append(pkg.Messages[0].Payloads, payload.NewPayload(data, payload.JSON))
	return pkg, nil
}

// DecodeCompletion borrows the whole package and never mutates or consumes it.
// The actual ingress must come from native transport, never serialized input.
func DecodeCompletion(pkg *relay.Package, localNode pid.NodeID) (Completion, error) {
	identity, err := decodeControl(pkg, localNode, ExitTopic, 2)
	if err != nil || identity.Kind != exitedControl {
		return Completion{}, ErrInvalidControl
	}
	value := pkg.Messages[0].Payloads[1]
	if value == nil || value.Format() != payload.JSON {
		return Completion{}, ErrInvalidResult
	}
	data, ok := value.Data().([]byte)
	if !ok {
		return Completion{}, ErrInvalidResult
	}
	result, err := DecodeResult(data)
	if err != nil {
		return Completion{}, err
	}
	identity.Kind = MonitorControl
	return Completion{Monitor: identity, Result: result}, nil
}
