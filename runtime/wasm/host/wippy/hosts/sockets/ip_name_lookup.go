// SPDX-License-Identifier: MPL-2.0

package sockets

import (
	"context"
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/dispatcher"
	socketapi "github.com/wippyai/runtime/api/socket"
	"github.com/wippyai/runtime/runtime/security"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const IPNameLookupNamespace = "wasi:sockets/ip-name-lookup@0.2.8"

// IPNameLookupHost implements wasi:sockets/ip-name-lookup@0.2.8.
type IPNameLookupHost struct {
	resources *preview2.ResourceTable
	// A host belongs to one serialized guest execution. The handle survives the
	// startup acknowledgement rewind; its resource owns the lookup operation.
	pendingResolve uint32
}

func NewIPNameLookupHost(resources *preview2.ResourceTable) *IPNameLookupHost {
	return &IPNameLookupHost{resources: resources}
}

func (h *IPNameLookupHost) Namespace() string {
	return IPNameLookupNamespace
}

// AsyncFunctions marks methods that use asyncify suspend/resume.
func (h *IPNameLookupHost) AsyncFunctions() []string {
	return []string{"resolve-addresses"}
}

// ResolveAddresses resolves a hostname to IP addresses.
func (h *IPNameLookupHost) ResolveAddresses(ctx context.Context, network uint32, name string) (uint32, *NetworkError) {
	async := wasmengine.GetAsyncify(ctx)
	if async != nil && async.IsRewinding(ctx) {
		return h.resumeResolveStart(ctx)
	}
	resource, ok := h.resources.Get(network)
	if !ok || resource.Type() != preview2.ResourceNetwork {
		panic("resolve-addresses: invalid network handle")
	}
	normalized, literal, validation := normalizeResolveName(name)
	if validation != nil {
		return 0, validation
	}
	if !security.IsAllowed(ctx, "socket.resolve", normalized, nil) {
		return 0, &NetworkError{Code: NetworkErrorAccessDenied}
	}
	if literal != nil {
		stream := preview2.NewResolveAddressStreamResource([]string{literal.String()})
		handle, err := h.resources.TryAdd(stream)
		if err != nil {
			stream.Drop()
			return 0, resourceLimitError(err)
		}
		return handle, nil
	}
	if async == nil {
		panic("resolve-addresses requires asyncify context")
	}
	if h.pendingResolve != 0 {
		panic("resolve-addresses: another startup is pending")
	}
	operation := socketapi.NewPendingOperation()
	stream := preview2.NewPendingResolveAddressStreamResource(operation)
	handle, err := h.resources.TryAdd(stream)
	if err != nil {
		stream.Drop()
		return 0, resourceLimitError(err)
	}
	h.pendingResolve = handle
	op := &resolvePendingOp{cmd: &socketapi.ResolveCmd{Host: normalized, Operation: operation, Timeout: wippyhost.GetCallLimits(ctx).EffectiveSocketTimeout()}}
	if err := wasmengine.Suspend(ctx, op); err != nil {
		h.pendingResolve = 0
		h.resources.Remove(handle)
		panic(fmt.Errorf("resolve-addresses suspend: %w", err))
	}
	return 0, nil
}

func (h *IPNameLookupHost) resumeResolveStart(ctx context.Context) (uint32, *NetworkError) {
	handle := h.pendingResolve
	h.pendingResolve = 0
	adopted := false
	defer func() {
		if !adopted && handle != 0 {
			h.resources.Remove(handle)
		}
	}()
	token, err := wasmengine.Resume(ctx)
	if err != nil {
		panic(fmt.Errorf("resolve-addresses resume: %w", err))
	}
	store := wippyhost.GetAsyncValueStore(ctx)
	if store == nil {
		panic("resolve-addresses: acknowledgement store missing")
	}
	value, ok := store.Take(token)
	if !ok {
		panic("resolve-addresses: acknowledgement missing")
	}
	ack, valid := value.(*socketapi.StartResult)
	if !valid || ack == nil {
		closeAsyncSocketResult(value)
		return 0, &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	if ack.Err != nil {
		return 0, mapNetError(ack.Err)
	}
	resource, ok := h.resources.Get(handle)
	if _, valid := resource.(*preview2.ResolveAddressStreamResource); !ok || !valid {
		panic("resolve-addresses: pending stream missing")
	}
	adopted = true
	return handle, nil
}

func (h *IPNameLookupHost) resolveStream(self uint32) *preview2.ResolveAddressStreamResource {
	resource, ok := h.resources.Get(self)
	stream, valid := resource.(*preview2.ResolveAddressStreamResource)
	if !ok || !valid {
		panic("invalid resolve-address-stream handle")
	}
	return stream
}

// [method]resolve-address-stream.resolve-next-address
func (h *IPNameLookupHost) MethodResolveAddressStreamResolveNextAddress(_ context.Context, self uint32) (*IPAddress, *NetworkError) {
	address, err, ready := h.resolveStream(self).Next()
	if !ready {
		return nil, &NetworkError{Code: NetworkErrorWouldBlock}
	}
	if err != nil {
		if errors.Is(err, preview2.ErrResolveLimit) || errors.Is(err, socketapi.ErrResolveLimit) {
			return nil, &NetworkError{Code: NetworkErrorOutOfMemory}
		}
		return nil, mapNetError(err)
	}
	if address == nil {
		return nil, nil
	}
	parsed := parseIPAddress(*address)
	if parsed == nil {
		return nil, &NetworkError{Code: NetworkErrorNameUnresolvable}
	}
	return parsed, nil
}

// [method]resolve-address-stream.subscribe
func (h *IPNameLookupHost) MethodResolveAddressStreamSubscribe(_ context.Context, self uint32) uint32 {
	pollable := h.resolveStream(self).Pollable()
	handle, err := h.resources.TryAdd(pollable)
	if err != nil {
		pollable.Drop()
		panic(fmt.Errorf("resolve-address-stream.subscribe: %w", err))
	}
	return handle
}

func (h *IPNameLookupHost) ResourceDropResolveAddressStream(_ context.Context, self uint32) {
	h.resolveStream(self)
	h.resources.Remove(self)
}

func (h *IPNameLookupHost) Register() map[string]any {
	return map[string]any{
		"resolve-addresses": wasmengine.CheckedHostFunction{Handler: h.ResolveAddresses, Validate: validateResolveNameMemory},
		"[method]resolve-address-stream.resolve-next-address": h.MethodResolveAddressStreamResolveNextAddress,
		"[method]resolve-address-stream.subscribe":            h.MethodResolveAddressStreamSubscribe,
		"[resource-drop]resolve-address-stream":               h.ResourceDropResolveAddressStream,
	}
}

type resolvePendingOp struct {
	cmd *socketapi.ResolveCmd
}

func (o *resolvePendingOp) CmdID() wasmengine.CommandID {
	return wasmengine.CommandID(socketapi.SocketResolve)
}

func (o *resolvePendingOp) ToCommand() dispatcher.Command {
	return o.cmd
}

func (o *resolvePendingOp) Execute(_ context.Context) (uint64, error) {
	return 0, fmt.Errorf("DNS resolve requires dispatcher")
}
