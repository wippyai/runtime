// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/registry"
)

// Fixtures declare who owns an entry with a "module" meta key and with
// Entry.DependencyRoot. linkFixtureProvenance moves those declarations into the
// provenance the stage reads and removes them from the entries, so a fixture
// can never make the stage read ownership from author payload.
const fixtureModuleKey = "module"

func fixtureEntryModule(entry registry.Entry) string {
	if entry.Meta == nil {
		return ""
	}
	module, _ := entry.Meta[fixtureModuleKey].(string)
	return module
}

func linkFixtureProvenance(prov registry.ProvMap, entries *[]registry.Entry) {
	for i := range *entries {
		entry := &(*entries)[i]
		record := registry.EntryProvenance{Module: fixtureEntryModule(*entry), Root: entry.DependencyRoot}
		entry.DependencyRoot = false
		delete(entry.Meta, fixtureModuleKey)
		prov[entry.ID] = record
	}
}

// linkFixtureStage builds a link stage over the provenance a fixture declares.
func linkFixtureStage(entries *[]registry.Entry, opts ...LinkOption) boot.Stage {
	prov := registry.ProvMap{}
	linkFixtureProvenance(prov, entries)
	return Link(prov, opts...)
}
