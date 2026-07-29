// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/cmd/internal/artifact"
	"github.com/wippyai/runtime/cmd/internal/artifact/nodepackage"
	"github.com/wippyai/wapp"
)

func newArtifactRegistry() (*artifact.Registry, error) {
	registry := artifact.NewRegistry()
	if err := registry.Register(nodepackage.New()); err != nil {
		return nil, fmt.Errorf("register built-in artifact format: %w", err)
	}
	return registry, nil
}

func validateArtifactResources(
	ctx context.Context,
	resources []wapp.ResourceSpec,
	moduleVersion string,
) error {
	registry, err := newArtifactRegistry()
	if err != nil {
		return err
	}
	_, err = artifact.InspectResources(ctx, registry, resources, moduleVersion)
	return err
}
