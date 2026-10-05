// SPDX-License-Identifier: MPL-2.0

//go:build !tailscale

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTailscaleDriverDisabled(t *testing.T) {
	require.Nil(t, newTailscaleDriver())
}
