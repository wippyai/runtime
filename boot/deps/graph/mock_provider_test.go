package graph

import (
	"context"
	"fmt"

	identityv1 "github.com/wippyai/module-registry-proto-go/registry/identity/v1"
	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
)

// mockProvider is a test implementation of ManifestProvider.
type mockProvider struct {
	modules map[string]*mockModule
	errors  map[string]error // errors to return for specific module names
}

type mockModule struct {
	name         Name
	labels       []*modulev1.Label
	manifests    map[string]*Manifest // version -> manifest
	organization *identityv1.Organization
	module       *modulev1.Module
}

// newMockProvider creates a new mock provider.
func newMockProvider() *mockProvider {
	return &mockProvider{
		modules: make(map[string]*mockModule),
		errors:  make(map[string]error),
	}
}

// addModule adds a module to the mock provider.
func (p *mockProvider) addModule(name Name) *mockModuleBuilder {
	return &mockModuleBuilder{
		provider: p,
		module: &mockModule{
			name:      name,
			labels:    make([]*modulev1.Label, 0),
			manifests: make(map[string]*Manifest),
			organization: &identityv1.Organization{
				Id:   name.Organization,
				Name: name.Organization,
			},
			module: &modulev1.Module{
				Id:             name.String(),
				OrganizationId: name.Organization,
				Name:           name.Module,
			},
		},
	}
}

// setError sets an error to be returned for a specific module.
func (p *mockProvider) setError(name Name, err error) {
	p.errors[name.String()] = err
}

// FetchManifests implements ManifestProvider.
func (p *mockProvider) FetchManifests(_ context.Context, requests []ManifestRequest) ([]ManifestResponse, error) {
	responses := make([]ManifestResponse, len(requests))

	for i, req := range requests {
		// Check for error
		if err, ok := p.errors[req.Name.String()]; ok {
			responses[i] = ManifestResponse{
				Request: req,
				Error:   err,
			}
			continue
		}

		// Get module
		mod, ok := p.modules[req.Name.String()]
		if !ok {
			responses[i] = ManifestResponse{
				Request: req,
				Error:   fmt.Errorf("module %s not found", req.Name),
			}
			continue
		}

		// Resolve version
		selectedLabel, err := resolveVersion(req.Constraint, mod.labels)
		if err != nil {
			responses[i] = ManifestResponse{
				Request: req,
				Error:   err,
			}
			continue
		}

		// Get manifest for version
		manifest := mod.manifests[selectedLabel.GetName()]

		responses[i] = ManifestResponse{
			Request:       req,
			Organization:  mod.organization,
			Module:        mod.module,
			Labels:        mod.labels,
			SelectedLabel: selectedLabel,
			Manifest:      manifest,
		}
	}

	return responses, nil
}

// mockModuleBuilder helps build mock modules fluently.
type mockModuleBuilder struct {
	provider *mockProvider
	module   *mockModule
}

// withVersion adds a version to the module.
func (b *mockModuleBuilder) withVersion(version, commitID string) *mockVersionBuilder {
	label := &modulev1.Label{
		Name:     version,
		CommitId: commitID,
		ModuleId: b.module.name.String(),
	}
	b.module.labels = append(b.module.labels, label)

	return &mockVersionBuilder{
		moduleBuilder: b,
		version:       version,
		manifest: &Manifest{
			Name:         b.module.name.String(),
			Version:      version,
			Dependencies: make([]ManifestDependency, 0),
		},
	}
}

// addModule adds another module to the provider.
func (b *mockModuleBuilder) addModule(name Name) *mockModuleBuilder {
	b.provider.modules[b.module.name.String()] = b.module
	return b.provider.addModule(name)
}

// build finalizes the module.
func (b *mockModuleBuilder) build() *mockProvider {
	b.provider.modules[b.module.name.String()] = b.module
	return b.provider
}

// mockVersionBuilder helps build version manifests.
type mockVersionBuilder struct {
	moduleBuilder *mockModuleBuilder
	version       string
	manifest      *Manifest
}

// withDependency adds a dependency to the manifest.
func (b *mockVersionBuilder) withDependency(name Name, constraint string) *mockVersionBuilder {
	b.manifest.Dependencies = append(b.manifest.Dependencies, ManifestDependency{
		Name:    name,
		Version: constraint,
	})
	return b
}

// withLocalDependency adds a local dependency (path-based).
func (b *mockVersionBuilder) withLocalDependency(name Name, path string) *mockVersionBuilder {
	b.manifest.Dependencies = append(b.manifest.Dependencies, ManifestDependency{
		Name: name,
		Path: path,
	})
	return b
}

// and continues building.
func (b *mockVersionBuilder) and() *mockModuleBuilder {
	b.moduleBuilder.module.manifests[b.version] = b.manifest
	return b.moduleBuilder
}

// withVersion adds another version to the same module.
func (b *mockVersionBuilder) withVersion(version, commitID string) *mockVersionBuilder {
	b.and()
	return b.moduleBuilder.withVersion(version, commitID)
}

// build finalizes the module.
func (b *mockVersionBuilder) build() *mockProvider {
	b.and()
	return b.moduleBuilder.build()
}
