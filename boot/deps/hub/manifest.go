// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	manifestv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/manifest/v1"
	modulev1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1"
	versionv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/version/v1"
)

type ResolvedModule struct {
	Org       string
	Name      string
	Version   string
	VersionID string
	Digest    string
	URL       string
	SizeBytes uint64
	Protected bool
}

type DependencySpec struct {
	Org        string
	Name       string
	Constraint string
}

type ResolutionError struct {
	Org        string
	Name       string
	Constraint string
	Message    string
}

func (e ResolutionError) String() string {
	module := e.Org + "/" + e.Name
	if e.Constraint != "" {
		return fmt.Sprintf("%s@%s: %s", module, e.Constraint, e.Message)
	}
	return fmt.Sprintf("%s: %s", module, e.Message)
}

type ResolveDependenciesResult struct {
	Modules []ResolvedModule
	Errors  []ResolutionError
}

// ModuleManifest holds the resolved manifest for a single module version.
type ModuleManifest struct {
	Org          string
	Name         string
	Version      string
	VersionID    string
	Digest       string
	URL          string
	Dependencies []ManifestDep
	SizeBytes    uint64
	Protected    bool
}

// ManifestDep represents a dependency declared by a resolved manifest.
type ManifestDep struct {
	Org       string
	Name      string
	Version   string
	VersionID string
	Digest    string
	URL       string
	SizeBytes uint64
	Protected bool
}

// GetManifest retrieves the manifest for a single module version. The
// resolution walk visits each transitive dependency once, so a transient
// network/5xx on any one of them aborts the whole install. Wrap the RPC
// in the shared retry helper so a hub flake doesn't make the user retype
// "wippy install" against a corp-flaky network.
func (c *Client) GetManifest(ctx context.Context, org, module, constraint string) (*ModuleManifest, error) {
	req := &manifestv1.GetManifestRequest{
		Module: &modulev1.ModuleRef{
			Value: &modulev1.ModuleRef_Name{
				Name: &modulev1.ModuleName{
					Org:  org,
					Name: module,
				},
			},
		},
	}

	if constraint != "" {
		req.Version = buildVersionRef(constraint)
	}

	var resp *connect.Response[manifestv1.GetManifestResponse]
	err := retryDo(ctx, DefaultRetryConfig(), func(_ int) error {
		r, err := c.Manifest.GetManifest(ctx, connect.NewRequest(req))
		if err != nil {
			return MapConnectError(err)
		}
		resp = r
		return nil
	})
	if err != nil {
		return nil, err
	}

	m := resp.Msg.Manifest
	if m == nil {
		return nil, ErrModuleNotFound
	}

	manifest := &ModuleManifest{
		Org:       m.Org,
		Name:      m.Name,
		Version:   m.Version,
		VersionID: m.VersionId,
		Digest:    m.Digest,
		SizeBytes: m.SizeBytes,
		Protected: m.Protected,
	}
	if m.Download != nil {
		manifest.URL = m.Download.Url
	}

	for _, dep := range m.Dependencies {
		md := ManifestDep{
			Org:       dep.Org,
			Name:      dep.Name,
			Version:   dep.Version,
			VersionID: dep.VersionId,
			Digest:    dep.Digest,
			SizeBytes: dep.SizeBytes,
			Protected: dep.Protected,
		}
		if dep.Download != nil {
			md.URL = dep.Download.Url
		}
		manifest.Dependencies = append(manifest.Dependencies, md)
	}

	return manifest, nil
}

func buildVersionRef(constraint string) *versionv1.VersionRef {
	if strings.HasPrefix(constraint, "@") {
		return &versionv1.VersionRef{
			Value: &versionv1.VersionRef_Label{
				Label: strings.TrimPrefix(constraint, "@"),
			},
		}
	}
	return &versionv1.VersionRef{
		Value: &versionv1.VersionRef_Version{
			Version: constraint,
		},
	}
}
