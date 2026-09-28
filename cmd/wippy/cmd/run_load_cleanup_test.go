// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	"go.uber.org/zap"
)

func TestPackBootstrapFailureStopsPreviouslyLoadedComponents(t *testing.T) {
	directory := t.TempDir()
	config := filepath.Join(directory, "wippy.yaml")
	require.NoError(t, os.WriteFile(config, []byte("version: \"1.0\"\n"), 0600))
	setTestConfigFiles(t, config)
	previous := nativeOptions
	t.Cleanup(func() { nativeOptions = previous })
	loaded, stopped := 0, 0
	failure := errors.New("injected component load failure")
	nativeOptions = &ExecuteOptions{
		LockFile: filepath.Join(directory, "wippy.lock"),
		Components: []boot.Component{
			boot.New(boot.P{Name: "cleanup-proof-owner", Load: func(ctx context.Context) (context.Context, error) {
				loaded++
				return ctx, nil
			}, Stop: func(ctx context.Context) error {
				require.NoError(t, ctx.Err(), "cleanup receives its own live context")
				stopped++
				return nil
			}}),
			boot.New(boot.P{Name: "cleanup-proof-failure", DependsOn: []string{"cleanup-proof-owner"}, Load: func(ctx context.Context) (context.Context, error) {
				return ctx, failure
			}}),
		},
	}
	loadedContext, loader, logger, registry, err := bootstrapPackRuntime(nil, zap.NewNop())
	require.ErrorContains(t, err, failure.Error())
	require.Nil(t, loadedContext)
	require.Nil(t, loader)
	require.Nil(t, logger)
	require.Nil(t, registry)
	require.Equal(t, 1, loaded)
	require.Equal(t, 1, stopped, "failed bootstrap must release prior component ownership")
}
