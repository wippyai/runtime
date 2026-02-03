package hub

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	manifestv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/manifest/v1"
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
			result.Errors = append(result.Errors, ResolutionError{
				Org:        e.Org,
				Name:       e.Name,
				Constraint: e.Constraint,
				Message:    e.Error.Message,
			})
		}
	}

	return result, nil
}
