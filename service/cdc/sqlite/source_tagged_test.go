// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/registry"
)

func TestBuildSourceWithTag(t *testing.T) {
	h, err := buildSource(sourceOptions{name: "x", dbResource: registry.NewID("app", "db")})
	require.NoError(t, err)
	require.NotNil(t, h)
	_, ok := h.(*Source)
	assert.True(t, ok)
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
