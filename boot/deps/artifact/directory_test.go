// SPDX-License-Identifier: MPL-2.0

package artifact

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
)

func TestDirectoryResourcesUsesRegistryOwner(t *testing.T) {
	root := t.TempDir()
	ctx := payload.WithTranscoder(ctxapi.NewRootContext(), directoryTranscoder{})
	meta := attrs.NewBagFrom(map[string]any{
		"module": "wrong/module",
		"artifact": map[string]any{
			"format": "node-package/v1",
		},
	})
	entries := registry.State{
		{
			ID:       registry.NewID("app", "selected"),
			Kind:     dirapi.Kind,
			Meta:     meta,
			Registry: registry.EntryMetadata{Owner: "org/selected"},
			Data: payload.New(dirapi.Config{
				Directory: root,
			}),
		},
		{
			ID:   registry.NewID("app", "unowned"),
			Kind: dirapi.Kind,
			Meta: meta,
			Data: payload.New(dirapi.Config{
				Directory: root,
			}),
		},
	}

	resources, err := DirectoryResources(ctx, entries,
		map[string]string{"org/selected": root},
		map[string]string{"org/selected": "1.2.3"},
	)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	require.Equal(t, "app:selected", resources[0].ResourceID.String())
	require.Equal(t, "1.2.3", resources[0].ModuleVersion)
}

type directoryTranscoder struct{}

func (directoryTranscoder) Unmarshal(value payload.Payload, target any) error {
	config, ok := target.(*dirapi.Config)
	if !ok {
		return errors.New("unexpected target")
	}
	source, ok := value.Data().(dirapi.Config)
	if !ok {
		return errors.New("unexpected payload")
	}
	*config = source
	return nil
}

func (directoryTranscoder) Transcode(value payload.Payload, _ payload.Format) (payload.Payload, error) {
	return value, nil
}
