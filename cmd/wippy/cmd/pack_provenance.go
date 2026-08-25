// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/wapp"
)

// encodePackProvenance serializes entry provenance into the pack metadata
// frame, keyed by canonical entry ID. The entries themselves stay verbatim;
// the pack carries the runtime's knowledge out of band.
func encodePackProvenance(prov registry.ProvenanceMap) map[string]any {
	out := make(map[string]any, len(prov))
	for id, p := range prov {
		rec := map[string]any{}
		if p.Module != "" {
			rec["module"] = p.Module
		}
		if p.Version != "" {
			rec["version"] = p.Version
		}
		if p.Digest != "" {
			rec["digest"] = p.Digest
		}
		if p.Root {
			rec["root"] = true
		}
		out[id.String()] = rec
	}
	return out
}

// packProvenanceFromMetadata reads the provenance a pack recorded in its
// metadata frame. A pack without the frame predates it and loads as
// host-authored entries; a pack WITH the frame is parsed strictly — a
// malformed record or an entry the frame does not cover is an error, never a
// silent host coercion.
func packProvenanceFromMetadata(reader *wapp.Reader, loadedEntries []registry.Entry) (registry.ProvenanceMap, error) {
	out := make(registry.ProvenanceMap, len(loadedEntries))
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
		var fieldErr error
		pr.Module, fieldErr = provString(rec, "module", fieldErr)
		pr.Version, fieldErr = provString(rec, "version", fieldErr)
		pr.Digest, fieldErr = provString(rec, "digest", fieldErr)
		pr.Root, fieldErr = provBool(rec, "root", fieldErr)
		if fieldErr != nil {
			return nil, fmt.Errorf("provenance record %q: %w", key, fieldErr)
		}
		if pr.Module == "" && (pr.Version != "" || pr.Digest != "") {
			return nil, fmt.Errorf("provenance record %q carries an artifact identity with no module", key)
		}
		byID[key] = pr
	}
	for _, entry := range loadedEntries {
		id := entry.ID.Canonical()
		pr, covered := byID[id.String()]
		if !covered {
			return nil, fmt.Errorf("provenance frame does not cover entry %s", id.String())
		}
		out[id] = pr
		delete(byID, id.String())
	}
	if len(byID) != 0 {
		extra := make([]string, 0, len(byID))
		for id := range byID {
			extra = append(extra, id)
		}
		sort.Strings(extra)
		return nil, fmt.Errorf("provenance frame contains record for unknown entry %s", extra[0])
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

// provString reads one optional string field, failing on a wrong type.
func provString(rec map[string]any, field string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	val, ok := rec[field]
	if !ok {
		return "", nil
	}
	str, isString := val.(string)
	if !isString {
		return "", fmt.Errorf("field %q has type %T, want string", field, val)
	}
	return str, nil
}

// provBool reads one optional bool field, failing on a wrong type.
func provBool(rec map[string]any, field string, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	val, ok := rec[field]
	if !ok {
		return false, nil
	}
	b, isBool := val.(bool)
	if !isBool {
		return false, fmt.Errorf("field %q has type %T, want bool", field, val)
	}
	return b, nil
}
