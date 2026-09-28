// SPDX-License-Identifier: MPL-2.0
package component

import (
	"context"
	"testing"

	"github.com/wippyai/runtime/api/registry"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

type scopeTestResources struct{ clears int }

func (r *scopeTestResources) Clear() { r.clears++ }

func TestHostRegistryForkOwnsResources(t *testing.T) {
	parent := NewHostRegistry()
	rootResources := &scopeTestResources{}
	parent.SetSharedResources(rootResources)
	err := parent.RegisterProfiles(HostProfile{Name: "test", Aliases: []string{"test:alias"}, Register: func(ctx context.Context, _ *wasmrt.Runtime) error {
		GetHostRegistry(ctx).SetSharedResources(&scopeTestResources{})
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	a, b := parent.Fork(), parent.Fork()
	if a.SharedResources() != nil || b.SharedResources() != nil {
		t.Fatal("fork inherited handles")
	}
	ctx := context.Background()
	ra, err := wasmrt.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer ra.Close(ctx)
	rb, err := wasmrt.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rb.Close(ctx)
	ids := []registry.ID{registry.ParseID("test:alias")}
	if err := a.EnsureImports(ctx, ra, ids, true); err != nil {
		t.Fatal(err)
	}
	if err := b.EnsureImports(ctx, rb, ids, true); err != nil {
		t.Fatal(err)
	}
	ar := a.SharedResources().(*scopeTestResources)
	br := b.SharedResources().(*scopeTestResources)
	if ar == br || ar == rootResources {
		t.Fatal("resource scopes shared")
	}
	a.CloseResources()
	a.CloseResources()
	if ar.clears != 1 || br.clears != 0 || rootResources.clears != 0 {
		t.Fatal("cleanup crossed resource scopes")
	}
	b.CloseResources()
}
