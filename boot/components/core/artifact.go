// SPDX-License-Identifier: MPL-2.0

package core

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/deps/artifact"
	"github.com/wippyai/runtime/boot/deps/artifact/standard"
)

// Artifacts composes the built-in and caller-provided artifact formats
// available to dependency lifecycle operations. Formats are explicit boot
// dependencies rather than globals.
func Artifacts(formats ...artifact.Format) boot.Component {
	return boot.New(boot.P{
		Name: ArtifactName,
		Load: func(ctx context.Context) (context.Context, error) {
			registry, err := standard.NewRegistry(formats...)
			if err != nil {
				return ctx, err
			}
			return artifact.WithRegistry(ctx, registry), nil
		},
	})
}
