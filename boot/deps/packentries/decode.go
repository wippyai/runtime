// SPDX-License-Identifier: MPL-2.0

// Package packentries decodes the registry entries a .wapp pack carries.
package packentries

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	"github.com/wippyai/wapp"
)

// Decode returns the registry entries of a pack. An fs.embed entry that
// carries no content digest gets the digest of its pack resource, computed by
// embedapi.ContentDigest exactly as the packer stamps it, so an embedded
// filesystem's identity is derived from its content whichever runtime
// published the pack.
func Decode(reader *wapp.Reader) ([]regapi.Entry, error) {
	wappEntries, err := reader.GetEntries()
	if err != nil {
		return nil, err
	}

	entries := make([]regapi.Entry, len(wappEntries))
	for i, we := range wappEntries {
		data := UnwrapPayloadData(we.Data)
		if we.Kind == embedapi.Kind {
			data, err = withContentDigest(reader, we.ID, data)
			if err != nil {
				return nil, err
			}
		}
		entries[i] = regapi.Entry{
			ID:   regapi.NewID(we.ID.Namespace, we.ID.Name),
			Kind: we.Kind,
			Meta: attrs.NewBagFrom(we.Meta),
			Data: payload.New(data),
		}
	}
	return entries, nil
}

func withContentDigest(reader *wapp.Reader, id wapp.ID, data any) (any, error) {
	config := map[string]any{}
	switch typed := data.(type) {
	case nil:
	case map[string]any:
		if digest, _ := typed["digest"].(string); digest != "" {
			return typed, nil
		}
		for key, value := range typed {
			config[key] = value
		}
	default:
		return nil, fmt.Errorf("fs.embed entry %s: unexpected data %T", id.String(), data)
	}

	resource, err := reader.GetFS(id)
	if err != nil {
		return nil, fmt.Errorf("fs.embed entry %s: pack resource: %w", id.String(), err)
	}
	digest, err := embedapi.ContentDigest(resource)
	if err != nil {
		return nil, fmt.Errorf("fs.embed entry %s: content digest: %w", id.String(), err)
	}
	config["digest"] = digest
	return config, nil
}

// UnwrapPayloadData extracts the inner data if the value is a serialized
// payload structure. Older packs stored the full payload wrapper.
func UnwrapPayloadData(data any) any {
	m, ok := data.(map[string]any)
	if !ok {
		return data
	}
	innerData, hasData := m["Data"]
	_, hasFormat := m["Format"]
	if hasData && hasFormat && len(m) == 2 {
		return innerData
	}
	return data
}
