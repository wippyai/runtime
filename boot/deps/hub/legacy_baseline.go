// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"encoding/hex"
	"strings"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
)

// legacyBaselineMatches verifies the runtime's old digest model using the
// current deployment identities and the exact recorded module artifacts. The
// old model included materialized root declarations, so authored replay alone
// can differ even when the files and lock have not changed. This runs only for
// mismatched resolutions; the model salt makes a v2 digest ineligible for a
// v1 match. Successful restore checkpoints the corrected model.
func (h *DependencyHandler) legacyBaselineMatches(
	ctx context.Context,
	target regapi.State,
	resolution *regapi.DependencyResolution,
	transcoder payload.Transcoder,
) (bool, error) {
	encoded := strings.TrimPrefix(resolution.BaselineDigest, "sha256:")
	if _, err := hex.DecodeString(encoded); err != nil || len(encoded) != 64 {
		return false, nil
	}
	matches, err := h.legacyDigestMatches(ctx, target, resolution.BaselineDigest, transcoder)
	if err != nil {
		return false, err
	}
	if matches {
		return h.legacySourceInputsMatch(ctx, transcoder)
	}
	resolved, err := resolvedModulesFromStored(resolution)
	if err != nil {
		return false, err
	}
	owners := make(map[string]struct{})
	for _, module := range resolved {
		name := module.Org + "/" + module.Name
		if h.isDeploymentRoot(name) {
			owners[name] = struct{}{}
		}
	}
	if len(owners) == 0 {
		return false, nil
	}
	entries, plan, err := h.loadModuleEntries(ctx, filterResolvedModules(resolved, owners), target, transcoder)
	if err != nil {
		return false, err
	}
	defer func() { _ = plan.cleanup() }()
	materialized := make(regapi.State, 0, len(target)+len(entries))
	for _, entry := range target {
		if _, replaced := owners[entryModule(entry)]; !replaced {
			materialized = append(materialized, entry)
		}
	}
	materialized = append(materialized, entries...)
	materialized, err = h.replayOwnedDependencyChanges(ctx, materialized, resolution, transcoder)
	if err != nil {
		return false, err
	}
	matches, err = h.legacyDigestMatches(ctx, materialized, resolution.BaselineDigest, transcoder)
	if err != nil || !matches {
		return false, err
	}
	return h.legacySourceInputsMatch(ctx, transcoder)
}

// legacyDigestMatches recognizes both the raw-lock recipe used before the
// separate deployment record and the canonical artifact identities thereafter.
func (h *DependencyHandler) legacyDigestMatches(ctx context.Context, state regapi.State, stored string, transcoder payload.Transcoder) (bool, error) {
	digest, err := h.hashDeploymentBaseline(ctx, state, transcoder, "deployment-overlay-v1")
	if err != nil || digest == stored || h.lock == nil || h.deployment == nil {
		return digest == stored, err
	}
	oldWriter := *h
	oldWriter.deployment = nil
	digest, err = oldWriter.hashDeploymentBaseline(ctx, state, transcoder, "deployment-overlay-v1")
	return digest == stored, err
}

// legacySourceInputsMatch verifies inputs that the old materialized-state hash
// could hide beneath history. Root-package declarations are checked against
// their immutable deployment artifact; source-owned declarations against the
// first recorded pre-edit entry. Without that evidence migration is ineligible
// and the normal deployment-refresh path remains authoritative.
func (h *DependencyHandler) legacySourceInputsMatch(ctx context.Context, transcoder payload.Transcoder) (bool, error) {
	baseline, ok := regapi.DependencyBaselineFromContext(ctx)
	if !ok {
		return false, nil
	}
	expected := make(map[regapi.ID]regapi.Entry)
	for _, entry := range baseline {
		if entry.Kind == regapi.NamespaceDependency && entry.Registry.Root {
			expected[entry.ID] = entry
		}
	}
	seen := make(map[regapi.ID]struct{})
	for _, transaction := range regapi.DependencyChangesFromContext(ctx) {
		for _, operation := range transaction {
			if _, checked := seen[operation.Entry.ID]; checked {
				continue
			}
			seen[operation.Entry.ID] = struct{}{}
			entry, present := expected[operation.Entry.ID]
			original := operation.OriginalEntry
			if (!present || h.isDeploymentRoot(entryModule(entry))) &&
				(original == nil || !original.Registry.Root || h.isDeploymentRoot(entryModule(*original))) {
				continue
			}
			if operation.Kind == regapi.EntryCreate {
				delete(expected, operation.Entry.ID)
			} else if original == nil {
				return false, nil
			} else {
				expected[operation.Entry.ID] = *original
			}
		}
	}
	var baselineModules []regapi.ResolvedModule
	if h.deployment != nil {
		baselineModules = h.deployment.Modules
	} else if h.lock != nil {
		for _, module := range h.lock.GetModules() {
			if !module.Root {
				continue
			}
			baselineModules = append(baselineModules, regapi.ResolvedModule{
				Name: module.Name, Version: module.Version, Source: moduleSourceHub, Digest: module.Hash,
			})
		}
	}
	resolved, err := resolvedModulesFromRecords(baselineModules)
	if err != nil {
		return false, err
	}
	owners := make(map[string]struct{})
	for _, module := range resolved {
		if name := module.Org + "/" + module.Name; h.isDeploymentRoot(name) {
			owners[name] = struct{}{}
		}
	}
	if len(owners) > 0 {
		entries, plan, err := h.loadModuleEntries(ctx, filterResolvedModules(resolved, owners), nil, transcoder)
		if err != nil {
			return false, err
		}
		defer func() { _ = plan.cleanup() }()
		for id, entry := range expected {
			if _, owned := owners[entryModule(entry)]; owned {
				delete(expected, id)
			}
		}
		for _, entry := range entries {
			if entry.Kind == regapi.NamespaceDependency && entry.Registry.Root {
				expected[entry.ID] = entry
			}
		}
	}
	inputs := make(regapi.State, 0, len(expected))
	for _, entry := range expected {
		inputs = append(inputs, entry)
	}
	want, err := h.hashDeploymentBaseline(ctx, inputs, transcoder, deploymentBaselineModel)
	if err != nil {
		return false, err
	}
	got, err := h.hashDeploymentBaseline(ctx, baseline, transcoder, deploymentBaselineModel)
	return got == want, err
}
