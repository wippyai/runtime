// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

func TestBuildSourceWithTag(t *testing.T) {
	h, err := buildSource(sourceOptions{name: "x", dbResource: registry.NewID("app", "db")})
	require.NoError(t, err)
	require.NotNil(t, h)
	_, ok := h.(*Source)
	assert.True(t, ok)
}

func TestSourceLifecycleRequiresDatabase(t *testing.T) {
	h, err := buildSource(sourceOptions{
		dbResource: registry.NewID("app", "db"),
		lifecycle:  supervisor.LifecycleConfig{Requires: []string{"app:other"}},
	})
	require.NoError(t, err)
	source := h.(*Source)
	first := source.LifecycleConfig()
	require.ElementsMatch(t, []string{"app:other", "app:db"}, first.RequiredServices())
	first.Requires[0] = "changed"
	require.ElementsMatch(t, []string{"app:other", "app:db"}, source.LifecycleConfig().RequiredServices())
	source.lifecycle.Requires = []string{"app:db"}
	require.Equal(t, []string{"app:db"}, source.LifecycleConfig().RequiredServices())
}

func TestBuildSourceRejectsBadInterval(t *testing.T) {
	_, err := buildSource(sourceOptions{name: "x", statusInterval: "nope"})
	assert.Error(t, err)
}

func TestBuildSourceRetainsSnapshotPolicyForPerSubscriberHandoff(t *testing.T) {
	h, err := buildSource(sourceOptions{name: "x", snapshot: true})
	require.NoError(t, err)
	assert.True(t, h.(*Source).snapshot)
}
