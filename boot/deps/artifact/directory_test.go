// SPDX-License-Identifier: MPL-2.0

package artifact

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
	syspayload "github.com/wippyai/runtime/system/payload"
	jsonpayload "github.com/wippyai/runtime/system/payload/json"
)

func directoryTestContext() context.Context {
	transcoder := syspayload.NewTranscoder()
	jsonpayload.Register(transcoder)
	return payload.WithTranscoder(ctxapi.NewRootContext(), transcoder)
}

func directoryEntry(id regapi.ID) regapi.Entry {
	return regapi.Entry{
		ID:   id,
		Kind: dirapi.Kind,
		Meta: attrs.NewBagFrom(map[string]any{
			"artifact": map[string]any{"format": "node-package"},
		}),
		Data: payload.New(map[string]any{"directory": "dist", "base": dirapi.BaseModule}),
	}
}

func TestDirectoryResources_SelectsByProvenance(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "dist"), 0o755))

	selected := directoryEntry(regapi.NewID("example.package", "artifact"))
	unrelated := directoryEntry(regapi.NewID("other.package", "artifact"))

	state := regapi.ProvenancedState{
		Entries: regapi.State{selected, unrelated},
		Prov: regapi.ProvMap{
			selected.ID:  {Module: "example/package", Version: "1.0.0"},
			unrelated.ID: {Module: "other/package", Version: "2.0.0"},
		},
	}

	resources, err := DirectoryResources(
		directoryTestContext(),
		state,
		map[string]string{"example/package": root},
		map[string]string{"example/package": "1.0.0"},
	)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, filepath.Join(root, "dist"), resources[0].Source)
	assert.Equal(t, "1.0.0", resources[0].ModuleVersion)
}

func TestDirectoryResources_MissingProvenanceFailsLoud(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "dist"), 0o755))

	entry := directoryEntry(regapi.NewID("example.package", "artifact"))
	state := regapi.ProvenancedState{Entries: regapi.State{entry}, Prov: regapi.ProvMap{}}

	_, err := DirectoryResources(
		directoryTestContext(),
		state,
		map[string]string{"example/package": root},
		map[string]string{"example/package": "1.0.0"},
	)
	require.ErrorIs(t, err, regapi.ErrMissingProvenance)
}
