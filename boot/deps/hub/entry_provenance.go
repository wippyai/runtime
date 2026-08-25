// SPDX-License-Identifier: MPL-2.0

package hub

import (
	regapi "github.com/wippyai/runtime/api/registry"
)

// Provenance is registry-owned. Ownership, resident artifact identity and
// deployment-root selection come from the ProvMap that travels with a state;
// entry Data and Meta are author payload and are never read for them.

type moduleOwner struct {
	name    string
	version string
	digest  string
}

func moduleOwnersByNamespace(modules []ResolvedModule) map[string]moduleOwner {
	owners := make(map[string]moduleOwner, len(modules))
	for _, mod := range modules {
		if mod.Org == "" || mod.Name == "" {
			continue
		}
		namespace := mod.Org + "." + mod.Name
		owners[namespace] = moduleOwner{
			name:    mod.Org + "/" + mod.Name,
			version: mod.Version,
			digest:  mod.Digest,
		}
	}
	return owners
}

// provIndex holds a state's provenance keyed the way the planner keys entries,
// so a directive input carrying ids that are not canonical yet still resolves.
type provIndex map[string]regapi.EntryProvenance

func provByKey(prov regapi.ProvMap) provIndex {
	index := make(provIndex, len(prov))
	for id, record := range prov {
		index[idKey(id)] = record
	}
	return index
}

// lookup returns the record for one entry. A missing record violates the
// total-map invariant: it is an error, never a host-authored default.
func (p provIndex) lookup(id regapi.ID) (regapi.EntryProvenance, error) {
	record, ok := p[idKey(id)]
	if !ok {
		return regapi.EntryProvenance{}, regapi.NewMissingProvenanceError(id)
	}
	return record, nil
}

// stateProvenance indexes a provenanced state and enforces the total-map
// invariant over its entries, so every later lookup in the same pass resolves.
func stateProvenance(state regapi.ProvenancedState) (provIndex, error) {
	index := provByKey(state.Prov)
	for _, entry := range state.Entries {
		if _, err := index.lookup(entry.ID); err != nil {
			return nil, err
		}
	}
	return index, nil
}

// operationProvenance returns the effective record for an operation's entry:
// the operation's own record when it carries one, the record already resident
// for that id otherwise, and an explicit host record for an entry that does not
// exist yet.
func operationProvenance(op regapi.Operation, resident provIndex) regapi.EntryProvenance {
	if op.Provenance != nil {
		return *op.Provenance
	}
	if record, ok := resident[idKey(op.Entry.ID)]; ok {
		return record
	}
	return regapi.EntryProvenance{}
}

// residentModuleVersions folds a state's provenance into the version of the
// artifact resident for each module. Entries of one module that disagree leave
// it without a resident version — the same unknown as a module whose records
// carry none.
func residentModuleVersions(prov regapi.ProvMap) map[string]string {
	versions := make(map[string]string)
	ambiguous := make(map[string]struct{})
	for _, record := range prov {
		if record.Module == "" || record.Version == "" {
			continue
		}
		if _, bad := ambiguous[record.Module]; bad {
			continue
		}
		if existing, seen := versions[record.Module]; seen && existing != record.Version {
			delete(versions, record.Module)
			ambiguous[record.Module] = struct{}{}
			continue
		}
		versions[record.Module] = record.Version
	}
	return versions
}

// residentModuleDigests folds a state's provenance into the digest of the
// artifact resident for each module, with the same disagreement rule as
// residentModuleVersions.
func residentModuleDigests(prov regapi.ProvMap) map[string]string {
	digests := make(map[string]string)
	ambiguous := make(map[string]struct{})
	for _, record := range prov {
		if record.Module == "" || record.Digest == "" {
			continue
		}
		if _, bad := ambiguous[record.Module]; bad {
			continue
		}
		if existing, seen := digests[record.Module]; seen && !artifactDigestsEqual(existing, record.Digest) {
			delete(digests, record.Module)
			ambiguous[record.Module] = struct{}{}
			continue
		}
		digests[record.Module] = record.Digest
	}
	return digests
}

// residentProvenanceAdvance returns the resident records that move for entries
// no operation touches. A module update whose entries are byte-identical
// produces no operation, yet its artifact identity did move: reporting it here
// advances the resident record with no entry event, so the next reconciliation
// compares an identity matching the selection and does not reload.
//
// An entry the planner deliberately left resident while its desired content
// differs is excluded: its bytes still come from the old artifact, so its
// record must keep naming that artifact.
func residentProvenanceAdvance(
	current, desired regapi.ProvenancedState,
	ops []regapi.Operation,
	skipKey string,
) regapi.ProvMap {
	touched := make(map[string]struct{}, len(ops))
	for _, op := range ops {
		touched[idKey(op.Entry.ID)] = struct{}{}
	}
	currentEntries := entriesByID(current.Entries)
	currentProv := provByKey(current.Prov)
	desiredProv := provByKey(desired.Prov)

	var advance regapi.ProvMap
	for _, entry := range desired.Entries {
		key := idKey(entry.ID)
		if key == skipKey {
			continue
		}
		if _, changed := touched[key]; changed {
			continue
		}
		resident, ok := currentEntries[key]
		if !ok {
			continue
		}
		record := desiredProv[key]
		if record == currentProv[key] || !entriesEqual(resident, entry) {
			continue
		}
		if advance == nil {
			advance = make(regapi.ProvMap)
		}
		advance[entry.ID] = record
	}
	return advance
}

// loadedProvenance is an ownership claim made while loading module artifacts:
// the record itself plus whether the claiming source is a local replacement.
type loadedProvenance struct {
	record      regapi.EntryProvenance
	replacement bool
}

// claimEntryProvenance resolves the owner of an entry a module artifact just
// produced. Ownership is decided in one pass over provenance data:
//
//   - a replacement is a mutable development source and is authoritative over
//     hub selection: its claim wins unconditionally, and no non-replacement
//     claim ever displaces a replacement-owned record;
//   - otherwise the record resident for that id keeps its owner, refreshed to
//     the loading module's identity when that owner is the loading module;
//   - otherwise the module declaring the entry's namespace owns it;
//   - otherwise the loading module owns it.
//
// Root is deployment-context state and is applied by the loader, which knows
// whether the loading module is the deployed application.
func claimEntryProvenance(
	entry regapi.Entry,
	loading moduleOwner,
	loadingIsReplacement bool,
	staged loadedProvenance,
	hasStaged bool,
	resident provIndex,
	namespaceOwners map[string]moduleOwner,
) loadedProvenance {
	claim := loadedProvenance{
		record:      regapi.EntryProvenance{Module: loading.name, Version: loading.version, Digest: loading.digest},
		replacement: loadingIsReplacement,
	}
	if loadingIsReplacement {
		return claim
	}
	if hasStaged && staged.replacement {
		return staged
	}
	if record, ok := resident[idKey(entry.ID)]; ok && record.Module != "" {
		if record.Module != loading.name {
			claim.record = regapi.EntryProvenance{Module: record.Module, Version: record.Version, Digest: record.Digest}
		}
		return claim
	}
	if owner, ok := namespaceOwners[entry.ID.NS]; ok && owner.name != "" {
		claim.record = regapi.EntryProvenance{Module: owner.name, Version: owner.version, Digest: owner.digest}
	}
	return claim
}
