// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"context"
	"errors"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	systemkv "github.com/wippyai/runtime/system/kv"
)

type contextTestReceiver struct{ err error }

func (r contextTestReceiver) Send(pkg *relay.Package) error {
	relay.ReleasePackage(pkg)
	return r.err
}

func TestRelayContextSendKeepsCallerOwnershipOnFailure(t *testing.T) {
	rejected := errors.New("receiver rejected")
	for _, tc := range []struct {
		want  error
		setup func(*relayRouter)
		name  string
	}{
		{errBlocked, func(r *relayRouter) {
			r.mesh.setDown("dst", true)
		}, "blocked link"},
		{nil, func(*relayRouter) {}, "missing destination"},
		{rejected, func(r *relayRouter) {
			r.register("dst", systemkv.KVRaftHostID, contextTestReceiver{err: rejected})
		}, "receiver rejection"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRelayRouter()
			r.mesh = newMesh()
			tc.setup(r)
			pkg := relay.NewServicePackage("src", systemkv.KVRaftHostID, "dst", systemkv.KVRaftHostID, "kv.authority", payload.New([]byte("body")))
			err := r.SendContext(context.Background(), pkg)
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("send error = %v, want %v", err, tc.want)
			}
			if pkg.Source.Node != "src" || len(pkg.Messages) != 1 || pkg.Messages[0].Topic != "kv.authority" {
				t.Fatalf("failed SendContext consumed caller package: %+v", pkg)
			}
			relay.ReleasePackage(pkg)
		})
	}
}
