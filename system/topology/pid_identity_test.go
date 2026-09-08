// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	topologyapi "github.com/wippyai/runtime/api/topology"
)

func TestPIDRegistrySameOwnerIgnoresStringCache(t *testing.T) {
	owner := pid.PID{Node: "node", Host: "process", UniqID: "owner"}
	for _, scope := range []string{"local", "global", "reservation", "eventual", "not-ready"} {
		t.Run(scope, func(t *testing.T) {
			cached := owner.Precomputed()
			r := NewPIDRegistry()
			switch scope {
			case "global":
				r = NewPIDRegistry(WithGlobalRegistry(&fakeGlobalRegistry{active: map[string]pid.PID{"name": cached}}))
			case "reservation":
				r = NewPIDRegistry(WithGlobalRegistry(&fakeGlobalRegistry{reserved: map[string]pid.PID{"name": cached}}))
			case "eventual":
				r = NewPIDRegistry(WithEventualRegistry(&fakeEventualRegistry{active: map[string]pid.PID{"name": cached}}))
			default:
				_, err := r.Register("name", cached)
				require.NoError(t, err)
				if scope == "not-ready" {
					r.SetGlobalRegistry(&fakeGlobalRegistry{notReady: true})
				}
			}
			_, err := r.Register("name", owner)
			require.NoError(t, err, "string caching must not change owner identity")
			for _, other := range []pid.PID{
				{Node: "other", Host: owner.Host, UniqID: owner.UniqID},
				{Node: owner.Node, Host: "other", UniqID: owner.UniqID},
				{Node: owner.Node, Host: owner.Host, UniqID: "other"},
			} {
				_, err := r.Register("name", other)
				if scope == "not-ready" {
					require.ErrorIs(t, err, topologyapi.ErrNameServiceNotReady)
				} else {
					require.ErrorIs(t, err, topologyapi.ErrNameAlreadyRegistered)
				}
			}
		})
	}
}
