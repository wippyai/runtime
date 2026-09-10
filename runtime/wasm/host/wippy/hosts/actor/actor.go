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
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/security"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const Namespace = "wippy:actor/process@0.1.0"

var errSchedulerRequired = errors.New("actor-scheduler-required")

type Host struct{ resources *preview2.ResourceTable }

// NewHost accepts the shared per-instance table used by wasi:io/poll. The
// optional form preserves existing direct construction for legacy actor calls.
// Subscribe deliberately requires an injected table: a private table would
// manufacture handles that wasi:io/poll cannot resolve or own at shutdown.
func NewHost(resources ...*preview2.ResourceTable) *Host {
	var table *preview2.ResourceTable
	if len(resources) != 0 {
		table = resources[0]
	}
	return &Host{resources: table}
}
func (*Host) Namespace() string        { return Namespace }
func (*Host) AsyncFunctions() []string { return []string{"send", "receive"} }

// Subscribe returns a standard wasi:io/poll pollable handle from this
// actor instance's shared ResourceTable. Readiness observes only mailbox state;
// it never consumes a queued message.
func (h *Host) Subscribe(ctx context.Context) uint32 {
	m := GetMailbox(ctx)
	if m == nil {
		panic(ErrActorRequired)
	}
	if h == nil || h.resources == nil {
		panic("actor poll resource table missing")
	}
	return h.resources.Add(m.Subscribe())
}

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
	msg, ok, err := m.takeValue()
	if err != nil {
		return Message{}, err
	}
	if ok {
		return msg, nil
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

// Send is the public, borrowed-input entry point. It snapshots values before
// an async send retains them, so direct Go callers and dynamic lowering may
// safely reuse their argument storage after Send returns.
func (h *Host) Send(ctx context.Context, target, topic string, inputs []Payload) (bool, error) {
	return h.send(ctx, target, topic, inputs, false)
}

// sendLifted is used only by the typed Canonical ABI binder. That binder fully
// lifts strings and list<u8> data into fresh Go storage before this method is
// entered, so SendCmd may adopt the payload bytes across Asyncify suspension.
func (h *Host) sendLifted(ctx context.Context, target, topic string, inputs []Payload) (bool, error) {
	return h.send(ctx, target, topic, inputs, true)
}

// send validates all values before allocating the command retained by the
// dispatcher. ownedInputs describes whether topic and payload data may be
// adopted; callers must not use it for borrowed storage.
func (h *Host) send(ctx context.Context, target, topic string, inputs []Payload, ownedInputs bool) (bool, error) {
	m := GetMailbox(ctx)
	if m == nil {
		return false, ErrActorRequired
	}
	async := wasmengine.GetAsyncify(ctx)
	if async != nil && async.IsRewinding(ctx) {
		return resumeSend(ctx)
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
	selfName := self.String()
	if !security.IsAllowed(ctx, "process.send", to.String(), map[string]any{"pid": selfName}) {
		return false, errors.New("denied")
	}
	if len(inputs) > MaxPayloads {
		return false, ErrTooLarge
	}
	n, err := messageHeaderSize(selfName, topic, len(inputs), m.limits.MessageBytes)
	if err != nil {
		return false, err
	}
	for _, input := range inputs {
		switch input.Format {
		case "bytes", "text", "json":
		default:
			return false, ErrUnsupportedPayload
		}
		size, err := encodedSize(input.Format, input.Data, "", m.limits.MessageBytes-n)
		if err != nil {
			return false, err
		}
		n += size
	}

	commandTopic := topic
	if !ownedInputs {
		commandTopic = strings.Clone(topic)
	}
	pls := make(payload.Payloads, len(inputs))
	for i, input := range inputs {
		format := payload.Bytes
		switch input.Format {
		case "text":
			format = payload.String
		case "json":
			format = payload.JSON
		}
		data := input.Data
		if !ownedInputs {
			data = append([]byte(nil), data...)
		}
		pls[i] = payload.NewPayload(data, format)
	}
	op := &sendPending{command: process.SendCmd{From: self, To: to, Topic: commandTopic, Payloads: pls}}
	if err := wasmengine.Suspend(ctx, op); err != nil {
		return false, err
	}
	return false, nil
}

// The pending operation and its dispatcher command share one owned lifetime.
// Returning the embedded command keeps that allocation alive through dispatch.
type sendPending struct{ command process.SendCmd }

func (*sendPending) CmdID() wasmengine.CommandID             { return wasmengine.CommandID(process.Send) }
func (s *sendPending) ToCommand() dispatcher.Command         { return &s.command }
func (*sendPending) Execute(context.Context) (uint64, error) { return 0, errSchedulerRequired }

// resumeSend consumes only the dispatcher completion; the send owns its inputs.
func resumeSend(ctx context.Context) (bool, error) {
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
