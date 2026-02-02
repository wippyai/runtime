package hub

import (
	"context"

	"connectrpc.com/connect"
	modulev1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1"
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
