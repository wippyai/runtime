package sockets

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/dispatcher"
	socketapi "github.com/wippyai/runtime/api/socket"
	"github.com/wippyai/runtime/runtime/security"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const IPNameLookupNamespace = "wasi:sockets/ip-name-lookup@0.2.0"

// IPNameLookupHost implements wasi:sockets/ip-name-lookup@0.2.0.
type IPNameLookupHost struct {
	resources *preview2.ResourceTable
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
func (h *IPNameLookupHost) ResolveAddresses(ctx context.Context, _ uint32, name string) (uint32, *NetworkError) {
	async := wasmengine.GetAsyncify(ctx)

	if async != nil && async.IsRewinding(ctx) {
		result, resumeErr := wasmengine.Resume(ctx)
		if resumeErr != nil {
			panic(fmt.Errorf("resolve-addresses resume: %w", resumeErr))
		}

		store := wippyhost.GetAsyncValueStore(ctx)
		if store == nil {
			panic("resolve-addresses: async value store not found")
		}

		data, ok := store.Take(result)
		if !ok {
			panic(fmt.Sprintf("resolve-addresses: token %d not found", result))
		}

		resolveResult := data.(*socketapi.ResolveResult)
		if resolveResult.Err != nil {
			return 0, mapNetError(resolveResult.Err)
		}

		stream := preview2.NewResolveAddressStreamResource(resolveResult.Addresses)
		handle := h.resources.Add(stream)
		return handle, nil
	}

	if !security.IsAllowed(ctx, "socket.resolve", name, nil) {
		return 0, &NetworkError{Code: NetworkErrorAccessDenied}
	}

	op := &resolvePendingOp{cmd: &socketapi.ResolveCmd{Host: name}}

	if async == nil {
		panic("resolve-addresses requires asyncify context")
	}

	if suspendErr := wasmengine.Suspend(ctx, op); suspendErr != nil {
		panic(fmt.Errorf("resolve-addresses suspend: %w", suspendErr))
	}

	return 0, nil
}

// [method]resolve-address-stream.resolve-next-address
func (h *IPNameLookupHost) MethodResolveAddressStreamResolveNextAddress(_ context.Context, self uint32) (*IPSocketAddress, *NetworkError) {
	r, ok := h.resources.Get(self)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}

	stream, ok := r.(*preview2.ResolveAddressStreamResource)
	if !ok {
		return nil, &NetworkError{Code: NetworkErrorInvalidArgument}
	}

	addr := stream.ReadNext()
	if addr == nil {
		return nil, nil
	}

	return &IPSocketAddress{
		Address: *addr,
		Port:    0,
	}, nil
}

// [method]resolve-address-stream.subscribe
func (h *IPNameLookupHost) MethodResolveAddressStreamSubscribe(_ context.Context, _ uint32) uint32 {
	pollable := &preview2.PollableResource{}
	pollable.SetReady(true)
	return h.resources.Add(pollable)
}

// ResourceDropResolveAddressStream drops a resolve address stream resource.
func (h *IPNameLookupHost) ResourceDropResolveAddressStream(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

func (h *IPNameLookupHost) Register() map[string]any {
	return map[string]any{
		"resolve-addresses": h.ResolveAddresses,
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
