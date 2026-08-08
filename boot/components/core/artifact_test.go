// SPDX-License-Identifier: MPL-2.0

package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/deps/artifact"
	"go.uber.org/zap"
)

type bootArtifactFormat struct{}

func (bootArtifactFormat) Name() string { return "test-format" }
func (bootArtifactFormat) Root() string { return "test-artifacts" }
func (bootArtifactFormat) Inspect(context.Context, artifact.InspectInput) (artifact.Descriptor, error) {
	return artifact.Descriptor{
		Identity:     "test",
		RelativePath: "test-artifacts/test",
	}, nil
}

func TestArtifactsBootRegistration(t *testing.T) {
	ctx, err := bootpkg.NewBootstrapContext(zap.NewNop(), nil)
	require.NoError(t, err)
	loader, err := bootpkg.NewLoader(Artifacts())
	require.NoError(t, err)
	ctx, err = loader.Load(ctx)
	require.NoError(t, err)

	registry := artifact.GetRegistry(ctx)
	require.NotNil(t, registry)
	_, registered := registry.Resolve("node-package")
	require.True(t, registered)
}

func TestArtifactsBootRegistrationIncludesProvidedFormats(t *testing.T) {
	ctx, err := bootpkg.NewBootstrapContext(zap.NewNop(), nil)
	require.NoError(t, err)
	loader, err := bootpkg.NewLoader(Artifacts(bootArtifactFormat{}))
	require.NoError(t, err)
	ctx, err = loader.Load(ctx)
	require.NoError(t, err)

	registry := artifact.GetRegistry(ctx)
	require.NotNil(t, registry)
	_, registered := registry.Resolve("test-format")
	require.True(t, registered)
}
