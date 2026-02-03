package graph

import (
	"context"

	identityv1 "github.com/wippyai/module-registry-proto-go/registry/identity/v1"
	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
)

// ManifestProvider fetches module manifests.
type ManifestProvider interface {
	// FetchManifests fetches manifests in batch.
	// Returns responses in the same order as requests.
	FetchManifests(ctx context.Context, requests []ManifestRequest) ([]ManifestResponse, error)
}

// ManifestRequest represents a request to fetch a module manifest.
type ManifestRequest struct {
	Name       Name
	Constraint string
}

// ManifestResponse contains the result of fetching a module manifest.
type ManifestResponse struct {
	Error         error
	Organization  *identityv1.Organization
	Module        *modulev1.Module
	SelectedLabel *modulev1.Label
	Manifest      *Manifest
	Request       ManifestRequest
	Labels        []*modulev1.Label
}

// Manifest represents a module's dependency manifest.
type Manifest struct {
	Name         string
	Version      string
	Dependencies []ManifestDependency
}

// ManifestDependency represents a dependency declared in a manifest.
type ManifestDependency struct {
	Parameters map[string]any
	Name       Name
	Version    string
	Path       string
}
