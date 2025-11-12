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
	Request ManifestRequest

	// Module information from API
	Organization *identityv1.Organization
	Module       *modulev1.Module
	Labels       []*modulev1.Label

	// Resolved version info (highest matching version)
	SelectedLabel *modulev1.Label

	// Dependencies from manifest
	Manifest *Manifest

	// Error if manifest couldn't be fetched
	Error error
}

// Manifest represents a module's dependency manifest.
type Manifest struct {
	Name         string
	Version      string
	Dependencies []ManifestDependency
}

// ManifestDependency represents a dependency declared in a manifest.
type ManifestDependency struct {
	Name       Name
	Version    string // constraint
	Path       string // local path (if local dependency)
	Parameters map[string]any
}
