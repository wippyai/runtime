// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/build/stages"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func BenchmarkExpandWithResidentRequirements(b *testing.B) {
	const size = 200
	vendorDir := filepath.Join(b.TempDir(), "vendor")
	component := []wapp.Entry{{ID: wapp.NewID("acme.component", "validator"), Kind: "function.lua",
		Data: map[string]any{"source": "return {}", "imports": map[string]any{}}}}
	for i := 0; i < size; i++ {
		name := fmt.Sprintf("fn%d", i)
		component = append(component,
			wapp.Entry{ID: wapp.NewID("acme.component", name), Kind: "function.lua", Data: map[string]any{"source": "return {}"}},
			wapp.Entry{ID: wapp.NewID("acme.component", name+"_req"), Kind: regapi.NamespaceRequirement,
				Data: map[string]any{"default": "x", "targets": []any{map[string]any{"entry": name, "path": ".value"}}}})
	}
	writeWapp(b, filepath.Join(vendorDir, "acme", "component-v1.0.0.wapp"), component)
	writeWapp(b, filepath.Join(vendorDir, "acme", "plain-v1.0.0.wapp"), []wapp.Entry{
		{ID: wapp.NewID("acme.plain", "fn"), Kind: "function.lua", Data: map[string]any{"source": "return {}"}},
	})
	writeWapp(b, filepath.Join(vendorDir, "acme", "workflows-v1.0.0.wapp"), []wapp.Entry{
		{ID: wapp.NewID("acme.workflows", "admission_import"), Kind: regapi.NamespaceRequirement,
			Data: map[string]any{"default": "acme.workflows:admission",
				"targets": []any{map[string]any{"entry": "acme.component:validator", "path": ".imports.workflow_admission"}}}},
		{ID: wapp.NewID("acme.workflows", "admission"), Kind: "function.lua", Data: map[string]any{"source": "return {}"}},
	})
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
			return &ModuleManifest{Org: org, Name: module, Version: version}, nil
		}},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(handler.manifestCache.Close)
	handler.deployment = &regapi.Deployment{Modules: []regapi.ResolvedModule{{Name: "acme/component", Version: "v1.0.0"}}}

	resident := []regapi.Entry{residentRoot("component", "acme/component")}
	for _, e := range component {
		resident = append(resident, regapi.Entry{ID: regapi.NewID(e.ID.Namespace, e.ID.Name), Kind: e.Kind,
			Data: payload.New(e.Data), Registry: regapi.EntryMetadata{Owner: "acme/component"}})
	}
	if err := stages.Link().Execute(newTestContext(), &resident); err != nil {
		b.Fatal(err)
	}
	for _, module := range []string{"plain", "workflows"} {
		b.Run(module, func(b *testing.B) {
			ctx := newTestContext()
			op := regapi.Operation{Kind: regapi.EntryCreate, Entry: residentRoot(module, "acme/"+module)}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := handler.Expand(ctx, op, resident); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
