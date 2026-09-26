// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/service/exec/native/internal/confinement"
)

func TestPlanBindMountsLayersNarrowGrants(t *testing.T) {
	mounts, err := PlanBindMounts(confinement.Filesystem{
		Read:  confinement.Access{Paths: []string{"/usr", "/srv/ws"}},
		Write: confinement.Access{Paths: []string{"/srv/ws/demo"}},
		Exec:  confinement.Access{Paths: []string{"/usr/bin"}},
	})
	require.NoError(t, err)
	require.Equal(t, []BindMount{
		{Source: "/usr", ReadOnly: true, NoExec: true},
		{Source: "/srv/ws", ReadOnly: true, NoExec: true},
		{Source: "/usr/bin", ReadOnly: true, NoExec: false},
		{Source: "/srv/ws/demo", ReadOnly: false, NoExec: true},
	}, mounts)
}

func TestPlanBindMountsRejectsUnboundOrUnrestrictedPolicy(t *testing.T) {
	_, err := PlanBindMounts(confinement.Filesystem{Read: confinement.Access{Unrestricted: true}})
	require.ErrorIs(t, err, ErrUnrestrictedMountClass)
	_, err = PlanBindMounts(confinement.Filesystem{Read: confinement.Access{Paths: []string{"/srv/../etc"}}})
	require.True(t, errors.Is(err, confinement.ErrInvalid))
}
