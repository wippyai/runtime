// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/relay"
	sysrelay "github.com/wippyai/runtime/system/relay"
)

func TestMonitorEndpointOwnedLifecycle(t *testing.T) {
	node := sysrelay.NewNode("local")
	sender := monitorContextSender(func(context.Context, *relay.Package) error { t.Fatal("unexpected send"); return nil })
	topo := NewTopology(sender, "local")
	cfg := MonitorConfig{MaxPending: 1, RequestTimeout: time.Second}
	stop, err := topo.StartRemoteMonitoring(context.Background(), node, cfg)
	require.NoError(t, err)
	endpoint, ok := node.GetHost(monitorControlHostID)
	require.True(t, ok)
	require.Same(t, topo.monitorEndpoint, endpoint)
	_, err = topo.StartRemoteMonitoring(context.Background(), node, cfg)
	require.Error(t, err)
	// A stale teardown must not remove a newer route owned by another component.
	node.UnregisterHost(monitorControlHostID)
	replacement := monitorContextSender(func(context.Context, *relay.Package) error { return nil })
	replacementRelease, err := node.RegisterOwnedHost(monitorControlHostID, replacement)
	require.NoError(t, err)
	defer replacementRelease()
	stop()
	stop()
	got, ok := node.GetHost(monitorControlHostID)
	require.True(t, ok)
	_, isReplacement := got.(monitorContextSender)
	require.True(t, isReplacement)
	require.True(t, topo.monitorEndpoint.stopped)
	_, err = topo.StartRemoteMonitoring(context.Background(), node, cfg)
	require.Error(t, err, "stopped endpoint cannot reopen")
}

func TestMonitorEndpointFailedRegistrationPreservesOwnerAndCanRetry(t *testing.T) {
	node := sysrelay.NewNode("local")
	sender := monitorContextSender(func(context.Context, *relay.Package) error { return nil })
	release, err := node.RegisterOwnedHost(monitorControlHostID, sender)
	require.NoError(t, err)
	topo := NewTopology(sender, "local")
	cfg := MonitorConfig{MaxPending: 1, RequestTimeout: time.Second}
	_, err = topo.StartRemoteMonitoring(context.Background(), node, cfg)
	require.Error(t, err)
	require.Nil(t, topo.monitorEndpoint)
	_, ok := node.GetHost(monitorControlHostID)
	require.True(t, ok)
	release()
	stop, err := topo.StartRemoteMonitoring(context.Background(), node, cfg)
	require.NoError(t, err)
	stop()
	_, ok = node.GetHost(monitorControlHostID)
	require.False(t, ok)
}
