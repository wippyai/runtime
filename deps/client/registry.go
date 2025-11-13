package client

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"connectrpc.com/connect"
	identityv1 "github.com/wippyai/module-registry-proto-go/registry/identity/v1"
	"github.com/wippyai/module-registry-proto-go/registry/identity/v1/identityv1connect"
	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
	"github.com/wippyai/module-registry-proto-go/registry/module/v1/modulev1connect"
)

// RegistryClient provides access to the module registry API.
type RegistryClient struct {
	organizationClient identityv1connect.OrganizationServiceClient
	moduleClient       modulev1connect.ModuleServiceClient
	labelClient        modulev1connect.LabelServiceClient
	downloadClient     modulev1connect.DownloadServiceClient
}

// NewRegistryClient creates a new registry client.
func NewRegistryClient(
	organizationClient identityv1connect.OrganizationServiceClient,
	moduleClient modulev1connect.ModuleServiceClient,
	labelClient modulev1connect.LabelServiceClient,
	downloadClient modulev1connect.DownloadServiceClient,
) *RegistryClient {
	return &RegistryClient{
		organizationClient: organizationClient,
		moduleClient:       moduleClient,
		labelClient:        labelClient,
		downloadClient:     downloadClient,
	}
}

// OrganizationInfo contains organization lookup results.
type OrganizationInfo struct {
	Name         string
	Organization *identityv1.Organization
}

// GetOrganizations fetches organizations by name.
func (c *RegistryClient) GetOrganizations(ctx context.Context, names []string) ([]OrganizationInfo, error) {
	if len(names) == 0 {
		return nil, nil
	}

	refs := make([]*identityv1.OrganizationRef, 0, len(names))
	for _, name := range names {
		refs = append(refs, &identityv1.OrganizationRef{
			Value: &identityv1.OrganizationRef_Name{Name: name},
		})
	}

	resp, err := c.organizationClient.ListOrganizations(ctx, connect.NewRequest(&identityv1.ListOrganizationsRequest{Refs: refs}))
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}

	result := make([]OrganizationInfo, 0, len(names))
	orgs := resp.Msg.GetOrganizations()

	for _, name := range names {
		i := slices.IndexFunc(orgs, func(o *identityv1.Organization) bool {
			return o.GetName() == name
		})
		if i == -1 {
			return nil, fmt.Errorf("organization %q not found", name)
		}
		result = append(result, OrganizationInfo{
			Name:         name,
			Organization: orgs[i],
		})
	}

	return result, nil
}

// ModuleInfo contains module lookup results.
type ModuleInfo struct {
	OrganizationID string
	Name           string
	Module         *modulev1.Module
}

// GetModules fetches modules by organization ID and module name.
func (c *RegistryClient) GetModules(ctx context.Context, requests []ModuleInfo) ([]ModuleInfo, error) {
	if len(requests) == 0 {
		return nil, nil
	}

	refs := make([]*modulev1.ModuleRef, 0, len(requests))
	for _, req := range requests {
		refs = append(refs, &modulev1.ModuleRef{
			Value: &modulev1.ModuleRef_NameRef{NameRef: &modulev1.ModuleRef_ModuleNameRef{
				OrganizationId: req.OrganizationID,
				Name:           req.Name,
			}},
		})
	}

	resp, err := c.moduleClient.ListModules(ctx, connect.NewRequest(&modulev1.ListModulesRequest{Refs: refs}))
	if err != nil {
		return nil, fmt.Errorf("list modules: %w", err)
	}

	result := make([]ModuleInfo, 0, len(requests))
	modules := resp.Msg.GetModules()

	for _, req := range requests {
		i := slices.IndexFunc(modules, func(m *modulev1.Module) bool {
			return m.GetName() == req.Name
		})
		if i == -1 {
			return nil, fmt.Errorf("module %q not found", req.Name)
		}
		result = append(result, ModuleInfo{
			OrganizationID: req.OrganizationID,
			Name:           req.Name,
			Module:         modules[i],
		})
	}

	return result, nil
}

// LabelInfo contains label lookup results for a module.
type LabelInfo struct {
	ModuleID string
	Labels   []*modulev1.Label
}

// GetLabels fetches all labels for the given module IDs.
func (c *RegistryClient) GetLabels(ctx context.Context, moduleIDs []string) ([]LabelInfo, error) {
	if len(moduleIDs) == 0 {
		return nil, nil
	}

	resp, err := c.labelClient.ListModuleLabels(ctx, connect.NewRequest(&modulev1.ListModuleLabelsRequest{ModuleIds: moduleIDs}))
	if err != nil {
		return nil, fmt.Errorf("list module labels: %w", err)
	}

	labelsMap := make(map[string][]*modulev1.Label)
	for _, label := range resp.Msg.GetLabels() {
		moduleID := label.GetModuleId()
		labelsMap[moduleID] = append(labelsMap[moduleID], label)
	}

	result := make([]LabelInfo, 0, len(moduleIDs))
	for _, moduleID := range moduleIDs {
		labels, ok := labelsMap[moduleID]
		if !ok {
			return nil, fmt.Errorf("no labels found for module ID %q", moduleID)
		}
		result = append(result, LabelInfo{
			ModuleID: moduleID,
			Labels:   labels,
		})
	}

	return result, nil
}

// DownloadResult contains the downloaded module content.
type DownloadResult struct {
	CommitID string
	Files    []*modulev1.File
}

// Download fetches module files for the given commit IDs.
func (c *RegistryClient) Download(ctx context.Context, commitIDs []string) ([]DownloadResult, error) {
	if len(commitIDs) == 0 {
		return nil, nil
	}

	resp, err := c.downloadClient.Download(ctx, connect.NewRequest(&modulev1.DownloadRequest{CommitIds: commitIDs}))
	if err != nil {
		return nil, fmt.Errorf("download commits: %w", err)
	}

	contents := resp.Msg.GetContents()
	if len(contents) == 0 {
		return nil, errors.New("no content downloaded")
	}

	result := make([]DownloadResult, 0, len(contents))
	for _, content := range contents {
		result = append(result, DownloadResult{
			CommitID: content.GetCommit().GetId(),
			Files:    content.GetFiles(),
		})
	}

	return result, nil
}
