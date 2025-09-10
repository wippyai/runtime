package moduleloader

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

// APIService handles all API requests to the module registry
type APIService struct {
	organizationClient identityv1connect.OrganizationServiceClient
	moduleClient       modulev1connect.ModuleServiceClient
	labelClient        modulev1connect.LabelServiceClient
}

// NewAPIService creates a new API service
func NewAPIService(
	organizationClient identityv1connect.OrganizationServiceClient,
	moduleClient modulev1connect.ModuleServiceClient,
	labelClient modulev1connect.LabelServiceClient,
) *APIService {
	return &APIService{
		organizationClient: organizationClient,
		moduleClient:       moduleClient,
		labelClient:        labelClient,
	}
}

// FetchDependencyInformation fetches all information needed for dependencies
func (api *APIService) FetchDependencyInformation(ctx context.Context, deps map[ManifestDependency]*dependencyInformation) error {
	err := errors.Join(
		api.fetchOrganizations(ctx, deps),
		api.fetchModules(ctx, deps),
		api.fetchLabels(ctx, deps),
	)
	if err != nil {
		return fmt.Errorf("fetch dependency information: %w", err)
	}
	return nil
}

// fetchOrganizations fetches organization information for dependencies
func (api *APIService) fetchOrganizations(ctx context.Context, deps map[ManifestDependency]*dependencyInformation) error {
	// Collect unique organization names
	organizationNames := make([]*identityv1.OrganizationRef, 0, len(deps))
	seen := make(map[string]struct{})
	for md := range deps {
		if _, ok := seen[md.Name.Organization]; ok {
			continue
		}
		organizationNames = append(organizationNames, &identityv1.OrganizationRef{
			Value: &identityv1.OrganizationRef_Name{Name: md.Name.Organization},
		})
		seen[md.Name.Organization] = struct{}{}
	}

	// Fetch organizations
	resp, err := api.organizationClient.ListOrganizations(ctx, connect.NewRequest(&identityv1.ListOrganizationsRequest{Refs: organizationNames}))
	if err != nil {
		return fmt.Errorf("list organizations: %w", err)
	}
	listedOrganizations := resp.Msg.GetOrganizations()

	// Map organizations to dependencies
	for md, info := range deps {
		organizationName := md.Name.Organization
		matchesName := func(o *identityv1.Organization) bool { return organizationName == o.GetName() }
		i := slices.IndexFunc(listedOrganizations, matchesName)
		if i == -1 {
			return fmt.Errorf("organization %s not found", organizationName)
		}
		info.organization = listedOrganizations[i]
	}
	return nil
}

// fetchModules fetches module information for dependencies
func (api *APIService) fetchModules(ctx context.Context, deps map[ManifestDependency]*dependencyInformation) error {
	// Build module references
	moduleRefs := make([]*modulev1.ModuleRef, 0, len(deps))
	for md, info := range deps {
		if info.organization == nil {
			return fmt.Errorf("missing organization info: %v", md)
		}
		moduleRefs = append(moduleRefs, &modulev1.ModuleRef{
			Value: &modulev1.ModuleRef_NameRef{NameRef: &modulev1.ModuleRef_ModuleNameRef{
				OrganizationId: info.organization.GetId(),
				Name:           md.Name.Module,
			}},
		})
	}

	// Fetch modules
	resp, err := api.moduleClient.ListModules(ctx, connect.NewRequest(&modulev1.ListModulesRequest{Refs: moduleRefs}))
	if err != nil {
		return fmt.Errorf("list modules: %w", err)
	}
	listedModules := resp.Msg.GetModules()

	// Map modules to dependencies
	for md, info := range deps {
		moduleName := md.Name.Module
		matchesName := func(m *modulev1.Module) bool { return moduleName == m.GetName() }
		i := slices.IndexFunc(listedModules, matchesName)
		if i == -1 {
			return fmt.Errorf("module %s not found", moduleName)
		}
		info.module = listedModules[i]
	}
	return nil
}

// fetchLabels fetches label information for dependencies
func (api *APIService) fetchLabels(ctx context.Context, deps map[ManifestDependency]*dependencyInformation) error {
	// Collect module IDs
	moduleIDs := make([]string, 0, len(deps))
	for md, info := range deps {
		if info.module == nil {
			return fmt.Errorf("missing module info: %v", md)
		}
		moduleIDs = append(moduleIDs, info.module.GetId())
	}

	// Fetch labels
	resp, err := api.labelClient.ListModuleLabels(ctx, connect.NewRequest(&modulev1.ListModuleLabelsRequest{ModuleIds: moduleIDs}))
	if err != nil {
		return fmt.Errorf("list module labels: %w", err)
	}
	listedLabels := resp.Msg.GetLabels()

	// Group labels by module ID
	moduleIDLabels := make(map[string][]*modulev1.Label, len(listedLabels))
	for _, label := range listedLabels {
		moduleIDLabels[label.GetModuleId()] = append(moduleIDLabels[label.GetModuleId()], label)
	}

	// Map labels to dependencies
	for md, info := range deps {
		moduleID := info.module.GetId()
		labels, ok := moduleIDLabels[moduleID]
		if !ok {
			return fmt.Errorf("missing module labels: %v", md)
		}
		info.labels = labels
	}
	return nil
}
