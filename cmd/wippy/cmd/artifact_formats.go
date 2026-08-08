// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/wippyai/runtime/boot/deps/artifact"
	"github.com/wippyai/runtime/boot/deps/artifact/standard"
	"github.com/wippyai/wapp"
)

func newArtifactRegistry() (*artifact.Registry, error) {
	return standard.NewRegistry()
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

func materializeArtifacts(
	ctx context.Context,
	packs []artifact.WAPP,
	resources []artifact.Resource,
	root string,
	exact bool,
) error {
	registry, err := newArtifactRegistry()
	if err != nil {
		return err
	}
	var effect *artifact.Effect
	if exact {
		effect, err = artifact.NewEffect(registry, packs, resources, root)
	} else {
		effect, err = artifact.NewPartialEffect(registry, packs, resources, root)
	}
	if err != nil {
		return fmt.Errorf("prepare module artifacts: %w", err)
	}
	if err := effect.Prepare(ctx); err != nil {
		return fmt.Errorf("materialize module artifacts: %w", err)
	}
	if err := effect.Commit(ctx); err != nil {
		rollbackErr := effect.Rollback(ctx)
		return fmt.Errorf("commit module artifacts: %w", errors.Join(err, rollbackErr))
	}
	if err := effect.Finalize(ctx); err != nil {
		return fmt.Errorf("finalize module artifacts: %w", err)
	}
	return nil
}
