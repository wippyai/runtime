// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/boot/deps/config"
)

func TestAddPublishedRuntimeMetadataRejectsManifestMachineLocalSections(t *testing.T) {
	for name, metadata := range map[string]attrs.Bag{
		"nested workspace": {"runtime": map[string]any{"workspace": map[string]any{
			"replacements": map[string]any{"acme/app": "../app"},
		}}},
		"dotted workspace":  {"runtime.workspace.replacements.acme/app": "../app"},
		"nested boot":       {"runtime": map[string]any{"boot": map[string]any{"config_dir": "/build"}}},
		"dotted extensions": {"runtime.extensions.paths": []any{"/build/ext"}},
	} {
		t.Run(name, func(t *testing.T) {
			err := addPublishedRuntimeMetadata(metadata, t.TempDir(), config.PublishConfig{})
			require.ErrorContains(t, err, "machine-local and cannot be published")
		})
	}
}

func TestAddPublishedRuntimeMetadataKeepsManifestRuntimeSections(t *testing.T) {
	metadata := attrs.Bag{"runtime": map[string]any{"lsp": map[string]any{"enabled": true}}}
	require.NoError(t, addPublishedRuntimeMetadata(metadata, t.TempDir(), config.PublishConfig{}))
	require.Equal(t, map[string]any{"lsp": map[string]any{"enabled": true}}, metadata["runtime"])
}
