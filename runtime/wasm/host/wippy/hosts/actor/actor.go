// SPDX-License-Identifier: MPL-2.0

// Package actor implements the language-neutral Wippy actor host interface.
package actor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/security"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
)

const Namespace = "wippy:actor/process@0.1.0"

var errSchedulerRequired = errors.New("actor-scheduler-required")

type Host struct{}

func NewHost() *Host                   { return &Host{} }
func (*Host) Namespace() string        { return Namespace }
func (*Host) AsyncFunctions() []string { return []string{"send", "receive"} }

func (*Host) Self(ctx context.Context) string {
	if GetMailbox(ctx) == nil {
		panic(ErrActorRequired)
	}
	self, ok := runtime.GetFramePID(ctx)
	if !ok {
		panic(ErrActorRequired)
	}
	return self.String()
}

func (*Host) TryReceive(ctx context.Context) (*Message, error) {
	m := GetMailbox(ctx)
	if m == nil {
		return nil, ErrActorRequired
	}
	return m.Take()
}

func (*Host) Receive(ctx context.Context) (Message, error) {
	m := GetMailbox(ctx)
	if m == nil {
		return Message{}, ErrActorRequired
	}
	async := wasmengine.GetAsyncify(ctx)
	if async != nil && async.IsRewinding(ctx) {
		if _, err := wasmengine.Resume(ctx); err != nil {
			return Message{}, err
		}
	}
	msg, err := m.Take()
	if err != nil {
		return Message{}, err
	}
	if msg != nil {
		return *msg, nil
	}
	if async == nil {
		return Message{}, errSchedulerRequired
	}
	if err := wasmengine.Suspend(ctx, &ReceivePending{}); err != nil {
		return Message{}, err
	}
	return Message{}, nil // ignored while the guest stack unwinds
}

// ReceivePending is a local mailbox wait. The runtime adapter parks on its
// actor inbox; it must not submit this operation to the external dispatcher.
type ReceivePending struct{}

func (*ReceivePending) CmdID() wasmengine.CommandID             { return 0 }
func (*ReceivePending) Execute(context.Context) (uint64, error) { return 0, errSchedulerRequired }

func (*Host) Send(ctx context.Context, target, topic string, inputs []Payload) (bool, error) {
	m := GetMailbox(ctx)
	if m == nil {
		return false, ErrActorRequired
	}
	async := wasmengine.GetAsyncify(ctx)
	if async != nil && async.IsRewinding(ctx) {
		token, err := wasmengine.Resume(ctx)
		if err != nil {
			return false, err
		}
		store := wippyhost.GetAsyncValueStore(ctx)
		if store == nil {
			return false, errSchedulerRequired
		}
		value, ok := store.Take(token)
		if !ok {
			return false, errors.New("invalid-send-completion")
		}
		result, ok := value.(process.SendResult)
		if !ok {
			return false, fmt.Errorf("invalid-send-completion: %T", value)
		}
		return result.Error == nil, result.Error
	}
	if async == nil {
		return false, errSchedulerRequired
	}
	if len(target) == 0 || len(target) > MaxPIDBytes {
		return false, errors.New("invalid-target")
	}
	to, err := pid.ParsePID(target)
	if err != nil || to.Node == "" || to.Host == "" || to.UniqID == "" {
		return false, errors.New("invalid-target")
	}
	self, ok := runtime.GetFramePID(ctx)
	if !ok {
		return false, ErrActorRequired
	}
	if !security.IsAllowed(ctx, "process.send", to.String(), map[string]any{"pid": self.String()}) {
		return false, errors.New("denied")
	}
	if len(inputs) > MaxPayloads {
		return false, ErrTooLarge
	}
	pls := make(payload.Payloads, len(inputs))
	for i, input := range inputs {
		var format string
		switch input.Format {
		case "bytes":
			format = payload.Bytes
		case "text":
			format = payload.String
		case "json":
			format = payload.JSON
		default:
			return false, ErrUnsupportedPayload
		}
		pls[i] = payload.NewPayload(input.Data, format)
	}
	if _, err := messageSize(self.String(), &relay.Message{Topic: topic, Payloads: pls}, m.limits.MessageBytes); err != nil {
		return false, err
	}
	// Canonical ABI input slices can reference guest memory. Snapshot before
	// suspending; dispatcher execution happens after this host invocation ends.
	for i, input := range inputs {
		pls[i] = payload.NewPayload(append([]byte(nil), input.Data...), pls[i].Format())
	}
	op := &sendPending{command: &process.SendCmd{From: self, To: to, Topic: strings.Clone(topic), Payloads: pls}}
	if err := wasmengine.Suspend(ctx, op); err != nil {
		return false, err
	}
	return false, nil
}

type sendPending struct{ command *process.SendCmd }

func (*sendPending) CmdID() wasmengine.CommandID             { return wasmengine.CommandID(process.Send) }
func (s *sendPending) ToCommand() dispatcher.Command         { return s.command }
func (*sendPending) Execute(context.Context) (uint64, error) { return 0, errSchedulerRequired }
