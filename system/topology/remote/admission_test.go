// SPDX-License-Identifier: MPL-2.0

package remote

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/relay"
)

func TestControlAuthenticationDoesNotGrantMonitorAuthority(t *testing.T) {
	control := testControl()
	authority, err := NewAuthority(control.Target.Node, 1)
	require.NoError(t, err)
	defer authority.Close()
	token, err := authority.Grant(GrantSpec{PeerNode: control.Watcher.Node, Watcher: control.Watcher, Target: control.Target, Expires: time.Now().Add(time.Minute)})
	require.NoError(t, err)
	control.Grant = token.String()
	connection := make(chan struct{})
	admitted := 0
	for _, authorizedActor := range []bool{false, true} {
		request := control
		if !authorizedActor {
			request.Watcher.UniqID = "same-peer-other-actor"
		}
		pkg, err := EncodeControl(request)
		require.NoError(t, err)
		pkg.Ingress = relay.IngressIdentity{Node: request.Watcher.Node, Authenticated: true, IntegrityProtected: true, ConnectionClosed: connection}
		decoded, err := DecodeControl(pkg, control.Target.Node)
		require.NoError(t, err, "both envelopes have valid authenticated immediate-peer identity")
		grant, err := ParseToken(decoded.Grant)
		require.NoError(t, err)
		lease, err := authority.Acquire(grant, pkg.Ingress, decoded.Watcher, decoded.Target)
		relay.ReleasePackage(pkg)
		if !authorizedActor {
			require.ErrorIs(t, err, ErrGrantDenied)
			require.Nil(t, lease)
			require.Zero(t, admitted)
		} else {
			require.NoError(t, err)
			require.NoError(t, lease.Use(func() error { admitted++; return nil }))
		}
	}
	require.Equal(t, 1, admitted)
}
