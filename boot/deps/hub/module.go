// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"

	"connectrpc.com/connect"
	modulev1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1"
	versionv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/version/v1"
)

type SearchResult struct {
	Modules    []*ModuleInfo
	TotalCount int32
}

type ModuleInfo struct {
	ID            string
	Org           string
	Name          string
	Description   string
	LatestVersion string
	Downloads     uint64
	Deprecated    bool
}

type SearchParams struct {
	Query             string
	License           string
	Keywords          []string
	Page              int32
	PageSize          int32
	IncludeDeprecated bool
}

type GetReadmeParams struct {
	Org     string
	Module  string
	Version string
	Label   string
}

type ReadmeInfo struct {
	Content  string
	Filename string
	Version  string
}

func (c *Client) SearchModules(ctx context.Context, params *SearchParams) (*SearchResult, error) {
	req := &modulev1.SearchModulesRequest{
		Query:             params.Query,
		Page:              params.Page,
		PageSize:          params.PageSize,
		Keywords:          params.Keywords,
		License:           params.License,
		IncludeDeprecated: params.IncludeDeprecated,
	}

	resp, err := c.Module.SearchModules(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, MapConnectError(err)
	}

	result := &SearchResult{
		TotalCount: resp.Msg.Total,
		Modules:    make([]*ModuleInfo, 0, len(resp.Msg.Modules)),
	}

	for _, m := range resp.Msg.Modules {
		info := &ModuleInfo{
			ID:            m.Id,
			Org:           m.OrganizationName,
			Name:          m.Name,
			Description:   m.Description,
			LatestVersion: m.LatestVersion,
			Downloads:     m.TotalDownloads,
			Deprecated:    m.Deprecated,
		}

		result.Modules = append(result.Modules, info)
	}

	return result, nil
}

func (c *Client) GetModule(ctx context.Context, org, name string) (*ModuleInfo, error) {
	req := &modulev1.GetModuleRequest{
		Module: &modulev1.ModuleRef{
			Value: &modulev1.ModuleRef_Name{
				Name: &modulev1.ModuleName{
					Org:  org,
					Name: name,
				},
			},
		},
	}

	resp, err := c.Module.GetModule(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, MapConnectError(err)
	}

	m := resp.Msg.Module
	if m == nil {
		return nil, ErrModuleNotFound
	}

	return &ModuleInfo{
		ID:            m.Id,
		Org:           m.OrganizationName,
		Name:          m.Name,
		Description:   m.Description,
		LatestVersion: m.LatestVersion,
		Downloads:     m.TotalDownloads,
		Deprecated:    m.Deprecated,
	}, nil
}

// VersionInfo holds version metadata returned from ListAllVersions.
type VersionInfo struct {
	ID      string
	Version string
	Yanked  bool
}

const listVersionsPageSize = 100

// ListAllVersions paginates through all non-yanked versions for a module.
func (c *Client) ListAllVersions(ctx context.Context, org, module string) ([]VersionInfo, error) {
	var all []VersionInfo
	var page int32 = 1

	for {
		req := &modulev1.ListVersionsRequest{
			Module: &modulev1.ModuleRef{
				Value: &modulev1.ModuleRef_Name{
					Name: &modulev1.ModuleName{
						Org:  org,
						Name: module,
					},
				},
			},
			Page:     page,
			PageSize: listVersionsPageSize,
		}

		resp, err := c.Module.ListVersions(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, MapConnectError(err)
		}

		for _, v := range resp.Msg.Versions {
			if v.Yanked {
				continue
			}
			all = append(all, VersionInfo{
				ID:      v.Id,
				Version: v.Version,
			})
		}

		if len(resp.Msg.Versions) < listVersionsPageSize {
			break
		}
		page++
	}

	return all, nil
}

func (c *Client) GetReadme(ctx context.Context, params *GetReadmeParams) (*ReadmeInfo, error) {
	req := &modulev1.GetReadmeRequest{
		Module: &modulev1.ModuleRef{
			Value: &modulev1.ModuleRef_Name{
				Name: &modulev1.ModuleName{
					Org:  params.Org,
					Name: params.Module,
				},
			},
		},
	}

	if params.Version != "" {
		req.Version = &versionv1.VersionRef{
			Value: &versionv1.VersionRef_Version{
				Version: params.Version,
			},
		}
	} else if params.Label != "" {
		req.Version = &versionv1.VersionRef{
			Value: &versionv1.VersionRef_Label{
				Label: params.Label,
			},
		}
	}

	resp, err := c.Module.GetReadme(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, MapConnectError(err)
	}

	return &ReadmeInfo{
		Content:  resp.Msg.GetContent(),
		Filename: resp.Msg.GetFilename(),
		Version:  resp.Msg.GetVersion(),
	}, nil
}
