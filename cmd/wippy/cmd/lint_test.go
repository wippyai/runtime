// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"encoding/json"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	transcoder "github.com/wippyai/runtime/system/payload"
	payloadjson "github.com/wippyai/runtime/system/payload/json"
)

func makeLuaEntry(id registry.ID, imports map[string]registry.ID) registry.Entry {
	cfg := struct {
		Imports map[string]registry.ID `json:"imports,omitempty"`
		Source  string                 `json:"source"`
	}{
		Source:  "return {}",
		Imports: imports,
	}
	payloadjson.Register(transcoder.GlobalTranscoder())
	raw, err := json.Marshal(cfg)
	if err != nil {
		panic(err)
	}
	return registry.Entry{
		ID:   id,
		Kind: registry.Kind("library.lua"),
		Data: payload.NewPayload(raw, payload.JSON),
	}
}

func TestExpandLuaEntriesByImports_IncludesDeps(t *testing.T) {
	depID := registry.NewID("ns.dep", "dep")
	rootID := registry.NewID("ns.root", "root")
	dep := makeLuaEntry(depID, nil)
	root := makeLuaEntry(rootID, map[string]registry.ID{"dep": depID})

	all := []registry.Entry{root, dep}
	selected := []registry.Entry{root}

	expanded, reportSet := expandLuaEntriesByImports(all, selected)
	if !reportSet[rootID] {
		t.Fatalf("expected reportSet to include root")
	}
	if reportSet[depID] {
		t.Fatalf("did not expect reportSet to include dependency")
	}
	seen := map[registry.ID]bool{}
	for _, e := range expanded {
		seen[e.ID] = true
	}
	if !seen[rootID] || !seen[depID] {
		t.Fatalf("expected expanded entries to include root and dep; got %v", seen)
	}
}
