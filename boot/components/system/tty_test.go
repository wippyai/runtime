// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	processapi "github.com/wippyai/runtime/api/process"
	ttyapi "github.com/wippyai/runtime/api/tty"
	processsystem "github.com/wippyai/runtime/system/process"
	ttysystem "github.com/wippyai/runtime/system/tty"
)

func ttyBootContext() context.Context {
	ctx := ctxapi.NewRootContext()
	ctx = ctxapi.WithFrameResolvers(ctx, ctxapi.NewFrameResolvers())
	return processapi.WithLifecycleRegistry(ctx, processsystem.NewLifecycleRegistry())
}

func TestTTYBootRejectsPreinstalledService(t *testing.T) {
	ctx := ttyBootContext()
	existing := ttysystem.NewService()
	ctx = ttyapi.WithService(ctx, existing)
	t.Cleanup(func() { require.NoError(t, existing.Close()) })

	loaded, err := TTY().Load(ctx)
	require.Nil(t, loaded)
	require.ErrorIs(t, err, ErrTTYServiceAlreadyInstalled)
	require.Same(t, existing, ttyapi.GetService(ctx))
}

func TestTTYBootInstallsAndStopsOwnedService(t *testing.T) {
	component := TTY()
	loaded, err := component.Load(ttyBootContext())
	require.NoError(t, err)
	require.NotNil(t, ttyapi.GetService(loaded))
	require.NoError(t, component.(boot.Stopper).Stop(loaded))
}
