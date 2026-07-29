// SPDX-License-Identifier: MPL-2.0

package artifact

import (
	"context"
	"fmt"
	"strings"

	"github.com/wippyai/wapp"
)

// InspectedResource is a validated artifact resource.
type InspectedResource struct {
	Declaration Declaration
	Descriptor  Descriptor
	ID          wapp.ID
}

// InspectResources validates every resource that opts into artifact semantics.
// Ordinary embedded filesystems pass through untouched.
func InspectResources(
	ctx context.Context,
	registry *Registry,
	resources []wapp.ResourceSpec,
	moduleVersion string,
) ([]InspectedResource, error) {
	inspected := make([]InspectedResource, 0)
	destinations := make(map[string]wapp.ID)
	for _, resource := range resources {
		declaration, declared, err := ParseDeclaration(resource.Meta)
		if err != nil {
			return nil, fmt.Errorf("resource %s: %w", resource.ID.String(), err)
		}
		if !declared {
			continue
		}
		descriptor, err := registry.Inspect(ctx, declaration, InspectInput{
			Filesystem:    resource.FS,
			ModuleVersion: moduleVersion,
			ResourceID:    resource.ID,
		})
		if err != nil {
			return nil, err
		}
		destinationKey := strings.ToLower(descriptor.RelativePath)
		if previous, exists := destinations[destinationKey]; exists {
			return nil, fmt.Errorf(
				"artifact resources %s and %s materialize to the same path %q",
				previous.String(), resource.ID.String(), descriptor.RelativePath)
		}
		destinations[destinationKey] = resource.ID
		inspected = append(inspected, InspectedResource{
			ID:          resource.ID,
			Declaration: declaration,
			Descriptor:  descriptor,
		})
	}
	return inspected, nil
}
