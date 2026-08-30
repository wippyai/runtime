// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"context"
	_ "embed"
	"testing"

	"github.com/wippyai/runtime/api/registry"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

//go:embed testdata/sockets_probe.wasm
var socketsProbe []byte

func TestWASISocketsProfileBindsComponent(t *testing.T) {
	ctx := context.Background()
	rt, err := wasmrt.New(ctx)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	defer rt.Close(ctx)

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
		t.Fatalf("register imported hosts: %v", err)
	}

	module, err := rt.LoadComponent(ctx, socketsProbe)
	if err != nil {
		t.Fatalf("load socket component: %v", err)
	}
	if err := module.Compile(ctx); err != nil {
		t.Fatalf("bind socket component: %v", err)
	}
}
