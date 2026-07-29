// SPDX-License-Identifier: MPL-2.0

package core

import (
	"testing"

	"github.com/stretchr/testify/require"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/deps/artifact"
	"go.uber.org/zap"
)

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
