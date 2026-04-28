// SPDX-License-Identifier: MPL-2.0

package env

import (
	"testing"

	"github.com/stretchr/testify/require"
	bootapi "github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	bootpkg "github.com/wippyai/runtime/boot"
	bootcore "github.com/wippyai/runtime/boot/components/core"
	sysreg "github.com/wippyai/runtime/system/registry"
	"go.uber.org/zap"
)

func TestEnvDependencyPatterns(t *testing.T) {
	ctx, err := bootpkg.NewBootstrapContext(zap.NewNop(), bootapi.NewConfig())
	require.NoError(t, err)

	ctx, err = bootcore.Registry().Load(ctx)
	require.NoError(t, err)

	ctx, err = Composite().Load(ctx)
	require.NoError(t, err)
	ctx, err = Variable().Load(ctx)
	require.NoError(t, err)

	reg, ok := regapi.GetRegistry(ctx).(*sysreg.Reg)
	require.True(t, ok)
	resolver := reg.DependencyResolver()
	require.NotNil(t, resolver)

	routerDeps := resolver.Extract(regapi.Entry{
		ID:   regapi.NewID("app.env", "router"),
		Kind: "env.storage.router",
		Data: payload.New(map[string]any{
			"storages": []any{"app.env:memory", "app.env:os"},
		}),
	})
	require.ElementsMatch(t, []string{"app.env:memory", "app.env:os"}, routerDeps)

	variableDeps := resolver.Extract(regapi.Entry{
		ID:   regapi.NewID("app.env", "API_KEY"),
		Kind: "env.variable",
		Data: payload.New(map[string]any{
			"storage": "app.env:router",
		}),
	})
	require.ElementsMatch(t, []string{"app.env:router"}, variableDeps)
}
