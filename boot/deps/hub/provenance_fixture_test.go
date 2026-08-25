// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"testing"

	"github.com/wippyai/runtime/api/attrs"
	regapi "github.com/wippyai/runtime/api/registry"
)

// Fixtures declare who owns an entry with the keys below and with
// Entry.DependencyRoot. They exist only in fixtures: fixtureState moves each
// declaration into the provenance record the registry owns and removes it from
// the entry, so a test can never hand the handler an entry that carries its own
// ownership.
const (
	fixtureModuleKey        = "module"
	fixtureModuleVersionKey = "module_version"
	fixtureModuleDigestKey  = "module_digest"
)

// fixtureState builds the provenanced state a directive receives. The resulting
// provenance is total over the entries, as the registry guarantees.
func fixtureState(entries regapi.State) regapi.ProvenancedState {
	state := regapi.ProvenancedState{
		Entries: make(regapi.State, 0, len(entries)),
		Prov:    make(regapi.ProvMap, len(entries)),
	}
	for _, entry := range entries {
		clean, record := fixtureEntryProvenance(entry)
		state.Entries = append(state.Entries, clean)
		state.Prov[clean.ID] = record
	}
	return state
}

// fixtureOperation strips an operation's declaration into op.Provenance, the
// way a producer of module operations populates it.
func fixtureOperation(op regapi.Operation) regapi.Operation {
	clean, record := fixtureEntryProvenance(op.Entry)
	op.Entry = clean
	if record != (regapi.EntryProvenance{}) {
		op.Provenance = &record
	}
	return op
}

// fixtureProvenance is the record a fixture entry declares, without the entry.
func fixtureProvenance(entry regapi.Entry) regapi.EntryProvenance {
	_, record := fixtureEntryProvenance(entry)
	return record
}

func fixtureEntryProvenance(entry regapi.Entry) (regapi.Entry, regapi.EntryProvenance) {
	record := regapi.EntryProvenance{Root: entry.DependencyRoot}
	entry.DependencyRoot = false
	if entry.Meta == nil {
		return entry, record
	}
	meta := attrs.NewBagFrom(entry.Meta)
	record.Module = meta.GetString(fixtureModuleKey, "")
	record.Version = meta.GetString(fixtureModuleVersionKey, "")
	record.Digest = meta.GetString(fixtureModuleDigestKey, "")
	delete(meta, fixtureModuleKey)
	delete(meta, fixtureModuleVersionKey)
	delete(meta, fixtureModuleDigestKey)
	entry.Meta = meta
	return entry, record
}

// fixtureOwned declares an entry as produced by a module artifact.
func fixtureOwned(entry regapi.Entry, module, version, digest string) regapi.Entry {
	meta := attrs.NewBagFrom(entry.Meta)
	meta.Set(fixtureModuleKey, module)
	if version != "" {
		meta.Set(fixtureModuleVersionKey, version)
	}
	if digest != "" {
		meta.Set(fixtureModuleDigestKey, digest)
	}
	entry.Meta = meta
	return entry
}

// requireScopedDelete asserts the plan deletes one entry, whatever provenance
// the operation carries.
func requireScopedDelete(t *testing.T, ops []regapi.ScopedOperation, id regapi.ID) regapi.Operation {
	t.Helper()
	for _, scoped := range ops {
		if scoped.Operation.Kind == regapi.EntryDelete && idsEqual(scoped.Operation.Entry.ID, id) {
			return scoped.Operation
		}
	}
	t.Fatalf("no delete operation for %s", id.String())
	return regapi.Operation{}
}
