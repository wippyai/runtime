// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/cluster/internode"
)

func TestRemoteMonitorControlNativeValidation(t *testing.T) {
	caller := pid.PID{Node: "remote", Host: "sysreg"}
	target := pid.PID{Node: "local", Host: "process", UniqID: "target"}
	for _, mode := range []string{"valid", "peer-spoof", "source-spoof", "target-spoof", "wrong-version", "missing-ref", "oversized-ref", "unknown-field", "mixed"} {
		t.Run(mode, func(t *testing.T) {
			fields := map[string]any{"v": remoteMonitorVersion, "ref": "relationship-one", "previous": "", "kind": topapi.MonitorRequest, "caller": caller, "target": target}
			if mode == "wrong-version" {
				fields["v"] = uint64(2)
			}
			if mode == "missing-ref" {
				delete(fields, "ref")
			}
			if mode == "oversized-ref" {
				fields["ref"] = string(make([]byte, 65))
			}
			if mode == "unknown-field" {
				fields["extra"] = true
			}
			pkg := relay.NewPackage(caller, target, topapi.TopicEvents, payload.New(fields))
			codec := internode.NewMessageCodec(nil)
			wire, err := codec.Encode(pkg)
			relay.ReleasePackage(pkg)
			require.NoError(t, err)
			pkg, err = codec.Decode(wire)
			require.NoError(t, err)
			defer relay.ReleasePackage(pkg)
			pkg.ReceivedFrom = "remote"
			if mode == "peer-spoof" {
				pkg.ReceivedFrom = "attacker"
			}
			if mode == "source-spoof" {
				pkg.Source.Host = "another"
			}
			if mode == "target-spoof" {
				pkg.Target.UniqID = "another"
			}
			if mode == "mixed" {
				pkg.AddMessage("data", payload.New("app"))
			}
			control, err := decodeRemoteMonitor(pkg, "local")
			if mode == "valid" {
				require.NoError(t, err)
				require.NotNil(t, control)
				require.Equal(t, "relationship-one", control.reference)
			} else {
				require.Error(t, err)
				require.Nil(t, control)
			}
		})
	}
}
