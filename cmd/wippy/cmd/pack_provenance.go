// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/wapp"
)

// encodePackProvenance serializes entry provenance into the pack metadata
// frame, keyed by canonical entry ID. The entries themselves stay verbatim;
// the pack carries the runtime's knowledge out of band.

// packProvenanceFromMetadata reads the provenance a pack recorded in its
// metadata frame. A pack without the frame predates it and loads as
// host-authored entries; a pack WITH the frame is parsed strictly — a
// malformed record or an entry the frame does not cover is an error, never a
// silent host coercion.
func packProvenanceFromMetadata(reader *wapp.Reader, loadedEntries []registry.Entry) (registry.ProvMap, error) {
	out := make(registry.ProvMap, len(loadedEntries))
	metadata, err := reader.GetMetadata()
	if err != nil {
		return nil, fmt.Errorf("read pack metadata: %w", err)
	}
	rawVal, framePresent := metadata["provenance"]
	if !framePresent {
		return out, nil
	}
	raw, ok := rawVal.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("provenance frame has type %T, want a map", rawVal)
	}
	byID := make(map[string]registry.EntryProvenance, len(raw))
	for key, val := range raw {
		rec, recOK := val.(map[string]any)
		if !recOK {
			return nil, fmt.Errorf("provenance record %q has type %T, want a map", key, val)
		}
		pr := registry.EntryProvenance{}
		pr.Module, _ = rec["module"].(string)
		pr.Version, _ = rec["version"].(string)
		pr.Digest, _ = rec["digest"].(string)
		pr.Root, _ = rec["root"].(bool)
		byID[key] = pr
	}
	for _, entry := range loadedEntries {
		id := entry.ID.Canonical()
		pr, covered := byID[id.String()]
		if !covered {
			return nil, fmt.Errorf("provenance frame does not cover entry %s", id.String())
		}
		out[id] = pr
	}
	return out, nil
}

// packFileDigest is the artifact identity of one pack file.
func packFileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
