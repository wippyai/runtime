package hub

import (
	"context"

	"connectrpc.com/connect"
	manifestv1 "git.spiralscout.com/wippy/proto-go/wippy/api/hub/manifest/v1"
)

type ResolvedModule struct {
	Org       string
	Name      string
	Version   string
	VersionID string
	Digest    string
	SizeBytes uint64
	URL       string
	Protected bool
}

type DependencySpec struct {
	Org        string
	Name       string
	Constraint string
}

type InstalledModule struct {
	Org     string
	Name    string
	Version string
	Digest  string
}

type ResolveDependenciesParams struct {
	Roots     []DependencySpec
	Installed []InstalledModule
}

type ResolveDependenciesResult struct {
	Modules []ResolvedModule
	Errors  []string
}

func (c *Client) ResolveDependencies(ctx context.Context, params *ResolveDependenciesParams) (*ResolveDependenciesResult, error) {
	roots := make([]*manifestv1.DependencySpec, 0, len(params.Roots))
	for _, r := range params.Roots {
		roots = append(roots, &manifestv1.DependencySpec{
			Org:        r.Org,
			Name:       r.Name,
			Constraint: r.Constraint,
		})
	}

	installed := make([]*manifestv1.InstalledModule, 0, len(params.Installed))
	for _, i := range params.Installed {
		installed = append(installed, &manifestv1.InstalledModule{
			Org:     i.Org,
			Name:    i.Name,
			Version: i.Version,
			Digest:  i.Digest,
		})
	}

	req := &manifestv1.ResolveDependenciesRequest{
		Roots:     roots,
		Installed: installed,
	}

	resp, err := c.Manifest.ResolveDependencies(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, MapConnectError(err)
	}

	result := &ResolveDependenciesResult{
		Modules: make([]ResolvedModule, 0, len(resp.Msg.Modules)),
	}

	for _, m := range resp.Msg.Modules {
		rm := ResolvedModule{
			Org:       m.Org,
			Name:      m.Name,
			Version:   m.Version,
			VersionID: m.VersionId,
			Digest:    m.Digest,
			SizeBytes: m.SizeBytes,
			Protected: m.Protected,
		}
		if m.Download != nil {
			rm.URL = m.Download.Url
		}
		result.Modules = append(result.Modules, rm)
	}

	for _, e := range resp.Msg.Errors {
		if e != nil && e.Error != nil {
			result.Errors = append(result.Errors, e.Error.Message)
		}
	}

	return result, nil
}
