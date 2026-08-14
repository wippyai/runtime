// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalOverlayOwner(t *testing.T) {
	owner, err := CanonicalOverlayOwner("  controller:one\t")
	require.NoError(t, err)
	assert.Equal(t, "controller:one", owner)

	_, err = CanonicalOverlayOwner(" \n\t")
	require.Error(t, err)
}
