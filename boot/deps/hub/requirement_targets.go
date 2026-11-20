// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"reflect"
	"strings"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/build/stages"
	"github.com/wippyai/runtime/system/entry"
)

// requirementTargetChanges lists the entries targeted by requirements that are
// added, removed or changed between two states, with their owner modules.
// Linking writes a requirement into its target, so such a target must be
// relinked from its raw artifact even when the owner artifact is unchanged.
type requirementTargetChanges struct {
	modules map[string]struct{}
	entries map[string]struct{}
}

func changedRequirementTargets(
	ctx context.Context,
	before regapi.State,
	after regapi.State,
	transcoder payload.Transcoder,
) (requirementTargetChanges, error) {
	changes := requirementTargetChanges{}
	count := 0
	for i := range before {
		if before[i].Kind == regapi.NamespaceRequirement {
			count++
		}
	}
	// Values index before; a negative value marks a requirement that is
	// present in both states.
	beforeReqs := make(map[requirementKey]int, count)
	for i := range before {
		if before[i].Kind == regapi.NamespaceRequirement {
			beforeReqs[requirementKey{before[i].ID.NS, before[i].ID.Name}] = i
		}
	}
	// Unchanged resident requirements share their payload with the desired
	// state, so the cheap equality check avoids decoding them.
	var candidates []regapi.Entry
	for i := range after {
		e := &after[i]
		if e.Kind != regapi.NamespaceRequirement {
			continue
		}
		key := requirementKey{e.ID.NS, e.ID.Name}
		index, ok := beforeReqs[key]
		if ok && index >= 0 {
			beforeReqs[key] = -1 - index
			old := &before[index]
			if samePayload(old.Data, e.Data) {
				continue
			}
			same, err := sameRequirementDefinition(ctx, *old, *e, transcoder)
			if err != nil {
				return changes, err
			}
			if same {
				continue
			}
			candidates = append(candidates, *old)
		}
		candidates = append(candidates, *e)
	}
	for _, index := range beforeReqs {
		if index >= 0 {
			candidates = append(candidates, before[index])
		}
	}
	if len(candidates) == 0 {
		return changes, nil
	}

	targetKeys := make(map[string]struct{})
	for _, req := range candidates {
		def, err := entry.DecodeEntryConfigRaw[stages.RequirementDefinition](ctx, transcoder, req)
		if err != nil {
			return changes, err
		}
		for _, target := range def.Targets {
			if id, ok := requirementTargetID(req.ID.NS, target.Entry); ok {
				targetKeys[idKey(id)] = struct{}{}
			}
		}
	}
	changes.modules = make(map[string]struct{})
	changes.entries = make(map[string]struct{}, len(targetKeys))
	for _, state := range []regapi.State{before, after} {
		for _, e := range state {
			key := idKey(e.ID)
			if _, targeted := targetKeys[key]; !targeted {
				continue
			}
			changes.entries[key] = struct{}{}
			if module := entryModule(e); module != "" {
				changes.modules[module] = struct{}{}
			}
		}
	}
	return changes, nil
}

func sameRequirementDefinition(ctx context.Context, a, b regapi.Entry, transcoder payload.Transcoder) (bool, error) {
	left, err := entry.DecodeEntryConfigRaw[stages.RequirementDefinition](ctx, transcoder, a)
	if err != nil {
		return false, err
	}
	right, err := entry.DecodeEntryConfigRaw[stages.RequirementDefinition](ctx, transcoder, b)
	if err != nil {
		return false, err
	}
	return reflect.DeepEqual(left, right), nil
}

// requirementTargetID mirrors the link stage: an unqualified target is
// relative to the requirement namespace.
func requirementTargetID(requirementNS, target string) (regapi.ID, bool) {
	target = strings.TrimSpace(target)
	if target == "" {
		return regapi.ID{}, false
	}
	ns, name, qualified := strings.Cut(target, ":")
	if !qualified {
		ns, name = requirementNS, target
	}
	return regapi.NewID(ns, name), true
}

type requirementKey struct{ ns, name string }

// samePayload compares only the requirement payload. Shared resident payloads
// compare by reference without decoding.
func samePayload(a, b payload.Payload) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Format() == b.Format() && reflect.DeepEqual(a.Data(), b.Data())
}
