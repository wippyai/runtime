// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"context"
	"os"
	"testing"

	"github.com/wippyai/runtime/api/registry"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

// The sockets host binds only if every one of its methods matches what the
// canonical-ABI lowering expects. finish-connect and accept used to return
// their handles as several bare values, which no guest could ever bind, so
// wasi:sockets TCP was dead end to end. This loads a real Go-authored
// component that imports those methods and fails if any of them cannot bind.
//
// The component is built from packages/xepozz/smtp/wasm-go in the neuro-brat
// deployment; point WASM_SOCKETS_PROBE at it to run this.
func TestWASISocketsProfileBindsAGoComponent(t *testing.T) {
	path := os.Getenv("WASM_SOCKETS_PROBE")
	if path == "" {
		t.Skip("set WASM_SOCKETS_PROBE to a component importing wasi:sockets")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read probe: %v", err)
	}

	ctx := context.Background()
	rt, err := wasmrt.New(ctx)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	defer func() { _ = rt.Close(ctx) }()

	hosts := wasmcomponent.NewHostRegistry()
	if err := hosts.RegisterProfiles(DefaultHostProfiles(zap.NewNop(), nil)...); err != nil {
		t.Fatalf("register profiles: %v", err)
	}

	imports := []registry.ID{
		registry.ParseID("wasi:io"),
		registry.ParseID("wasi:cli"),
		registry.ParseID("wasi:clocks"),
		registry.ParseID("wasi:filesystem"),
		registry.ParseID("wasi:random"),
		registry.ParseID("wasi:sockets"),
	}
	if err := hosts.EnsureImports(ctx, rt, imports, true); err != nil {
		t.Fatalf("bind hosts: %v", err)
	}

	module, err := rt.LoadComponent(ctx, data)
	if err != nil {
		t.Fatalf("load component: %v", err)
	}
	if err := module.Compile(ctx); err != nil {
		t.Fatalf("compile component: %v", err)
	}
}
