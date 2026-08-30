// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	regapi "github.com/wippyai/runtime/api/registry"
)

func TestRegistryListMetadataIsOptIn(t *testing.T) {
	entry := regapi.Entry{
		ID:       regapi.NewID("acme.module", "service"),
		Kind:     "service",
		Registry: regapi.EntryMetadata{Owner: "acme/module", Root: true},
	}

	without := registryEntryInfos([]regapi.Entry{entry}, false)
	require.Len(t, without, 1)
	assert.Nil(t, without[0].Registry)

	with := registryEntryInfos([]regapi.Entry{entry}, true)
	require.Len(t, with, 1)
	require.NotNil(t, with[0].Registry)
	assert.Equal(t, entry.Registry, *with[0].Registry)
	assert.Empty(t, entry.Meta, "formatting must not copy registry state into author metadata")
}
