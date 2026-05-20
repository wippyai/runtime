// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

// opTest is a helper to create test operations with less boilerplate
type opTest struct {
	kind      event.Kind
	ns        string
	name      string
	entryKind string
	data      string
	dependsOn []string
	groups    []string
}

func (ot opTest) toOperation() registry.Operation {
	entry := testEntry{
		ns:        ot.ns,
		name:      ot.name,
		kind:      ot.entryKind,
		data:      ot.data,
		dependsOn: ot.dependsOn,
		groups:    ot.groups,
	}.toEntry()

	return registry.Operation{
		Kind:  ot.kind,
		Entry: entry,
	}
}

// Fluent builders for operations
func createOp(ns, name string) *opBuilder {
	return &opBuilder{
		kind: registry.EntryCreate,
		ns:   ns,
		name: name,
	}
}

func updateOp(ns, name string) *opBuilder {
	return &opBuilder{
		kind: registry.EntryUpdate,
		ns:   ns,
		name: name,
	}
}

func deleteOp(ns, name string) *opBuilder {
	return &opBuilder{
		kind: registry.EntryDelete,
		ns:   ns,
		name: name,
	}
}

type opBuilder struct {
	kind      event.Kind
	ns        string
	name      string
	entryKind string
	data      string
	dependsOn []string
	groups    []string
}

func (ob *opBuilder) Kind(kind string) *opBuilder {
	ob.entryKind = kind
	return ob
}

func (ob *opBuilder) Data(data string) *opBuilder {
	ob.data = data
	return ob
}

func (ob *opBuilder) DependsOn(deps ...string) *opBuilder {
	ob.dependsOn = deps
	return ob
}

func (ob *opBuilder) Groups(groups ...string) *opBuilder {
	ob.groups = groups
	return ob
}

func (ob *opBuilder) Build() registry.Operation {
	if ob.entryKind == "" {
		ob.entryKind = "service"
	}
	if ob.data == "" {
		ob.data = "test-data"
	}

	return opTest{
		kind:      ob.kind,
		ns:        ob.ns,
		name:      ob.name,
		entryKind: ob.entryKind,
		data:      ob.data,
		dependsOn: ob.dependsOn,
		groups:    ob.groups,
	}.toOperation()
}

// ChangeSetBuilder helps build changesets for testing
type ChangeSetBuilder struct {
	operations []registry.Operation
}

func NewChangeSet() *ChangeSetBuilder {
	return &ChangeSetBuilder{
		operations: make([]registry.Operation, 0),
	}
}

func (csb *ChangeSetBuilder) Add(op registry.Operation) *ChangeSetBuilder {
	csb.operations = append(csb.operations, op)
	return csb
}

func (csb *ChangeSetBuilder) AddOp(builder *opBuilder) *ChangeSetBuilder {
	csb.operations = append(csb.operations, builder.Build())
	return csb
}

func (csb *ChangeSetBuilder) Build() registry.ChangeSet {
	return csb.operations
}

// verifyOperationOrder checks if operations respect their dependency order and operation type order
func verifyOperationOrder(t *testing.T, sorted registry.ChangeSet, checks []struct {
	operation       registry.Operation
	mustBeforeNames []string
	mustAfterNames  []string
}) {
	t.Helper()

	// Build map of operation positions
	posMap := make(map[string]int)

	for i, op := range sorted {
		key := op.Entry.ID.Name
		posMap[key] = i
	}

	// Check each dependency requirement
	for _, check := range checks {
		opName := check.operation.Entry.ID.Name
		opPos, exists := posMap[opName]
		if !exists {
			t.Errorf("operation %s not found in sorted changeset", opName)
			continue
		}

		// Check must-come-before relationships
		for _, mustAfterName := range check.mustBeforeNames {
			dependentPos, exists := posMap[mustAfterName]
			if !exists {
				t.Errorf("dependent operation %s not found in sorted changeset", mustAfterName)
				continue
			}

			if opPos >= dependentPos {
				t.Errorf("dependency order violation: %s (pos %d) must come before %s (pos %d)",
					opName, opPos, mustAfterName, dependentPos)
			}
		}

		// Check must-come-after relationships
		for _, mustBeforeName := range check.mustAfterNames {
			dependencyPos, exists := posMap[mustBeforeName]
			if !exists {
				t.Errorf("dependency operation %s not found in sorted changeset", mustBeforeName)
				continue
			}

			if opPos <= dependencyPos {
				t.Errorf("dependency order violation: %s (pos %d) must come after %s (pos %d)",
					opName, opPos, mustBeforeName, dependencyPos)
			}
		}
	}
}

func findOperationIndex(t *testing.T, sorted registry.ChangeSet, kind event.Kind, id registry.ID) int {
	t.Helper()
	for i, op := range sorted {
		if op.Kind == kind && op.Entry.ID.Equal(id) {
			return i
		}
	}
	t.Fatalf("operation %s %s not found in sorted changeset", kind, id.String())
	return -1
}

func assertOperationBefore(t *testing.T, sorted registry.ChangeSet, beforeKind event.Kind, beforeID registry.ID, afterKind event.Kind, afterID registry.ID) {
	t.Helper()
	before := findOperationIndex(t, sorted, beforeKind, beforeID)
	after := findOperationIndex(t, sorted, afterKind, afterID)
	if before >= after {
		t.Fatalf("expected %s %s before %s %s; sorted order:\n%s",
			beforeKind, beforeID.String(), afterKind, afterID.String(), formatDelta(sorted))
	}
}

func importResolver(t *testing.T) *Resolver {
	t.Helper()
	resolver := NewResolver()
	if err := resolver.RegisterPattern(registry.DependencyPattern{
		Path:          "data.imports.*",
		Description:   "function/library imports",
		AllowWildcard: true,
	}); err != nil {
		t.Fatalf("register import resolver: %v", err)
	}
	return resolver
}

func envResolver(t *testing.T) *Resolver {
	t.Helper()
	resolver := NewResolver()
	for _, pattern := range []registry.DependencyPattern{
		{Path: "data.storage", Description: "env variable storage backend"},
		{Path: "data.storages", Description: "env router backing storages", AllowWildcard: true},
	} {
		if err := resolver.RegisterPattern(pattern); err != nil {
			t.Fatalf("register env resolver pattern %s: %v", pattern.Path, err)
		}
	}
	return resolver
}

func importEntry(ns, name, kind, source string, imports map[string]string) registry.Entry {
	data := map[string]any{"source": source}
	if len(imports) > 0 {
		importData := make(map[string]any, len(imports))
		for alias, id := range imports {
			importData[alias] = id
		}
		data["imports"] = importData
	}
	return registry.Entry{
		ID:   registry.NewID(ns, name),
		Kind: kind,
		Data: payload.New(data),
		Meta: map[string]any{},
	}
}

func envEntry(ns, name, kind string, data map[string]any) registry.Entry {
	return registry.Entry{
		ID:   registry.NewID(ns, name),
		Kind: kind,
		Data: payload.New(data),
		Meta: map[string]any{},
	}
}

func envStorageGraph(ns string) (memory, osStorage, router, variable registry.Entry) {
	memory = envEntry(ns, "memory", "env.storage.memory", nil)
	osStorage = envEntry(ns, "os", "env.storage.os", nil)
	router = envEntry(ns, "router", "env.storage.router", map[string]any{
		"storages": []any{memory.ID.String(), osStorage.ID.String()},
	})
	variable = envEntry(ns, "ROUTER_TEST_VAR", "env.variable", map[string]any{
		"storage": router.ID.String(),
	})
	return memory, osStorage, router, variable
}

func applyWithIncomingDependencyCheck(builder *StateBuilder, fromState registry.State, sorted registry.ChangeSet) error {
	state := NewStateMap(fromState)

	for _, op := range sorted {
		if op.Kind == registry.EntryDelete {
			universe := dependencyUniverse(StateMapToSlice(state))
			for _, entry := range StateMapToSlice(state) {
				if entry.ID.Equal(op.Entry.ID) {
					continue
				}
				for _, depID := range builder.entryDependencyIDs(entry, universe) {
					if depID.Equal(op.Entry.ID) {
						return fmt.Errorf("delete %s before dependent %s moved/deleted",
							op.Entry.ID.String(), entry.ID.String())
					}
				}
			}
		}

		next, err := builder.ApplyOperation(state, op)
		if err != nil {
			return fmt.Errorf("sorted operation %s %s does not apply cleanly: %w",
				op.Kind, op.Entry.ID.String(), err)
		}
		state = next
	}

	return nil
}

func assertAppliesWithoutIncomingDependencyDelete(t *testing.T, builder *StateBuilder, fromState registry.State, sorted registry.ChangeSet) {
	t.Helper()
	if err := applyWithIncomingDependencyCheck(builder, fromState, sorted); err != nil {
		t.Fatalf("%v\nsorted order:\n%s", err, formatDelta(sorted))
	}
}

func assertCannotApplyWithoutIncomingDependencyDelete(t *testing.T, builder *StateBuilder, fromState registry.State, sorted registry.ChangeSet) {
	t.Helper()
	if err := applyWithIncomingDependencyCheck(builder, fromState, sorted); err == nil {
		t.Fatalf("expected sorted changeset to remain invalid, but it applied cleanly:\n%s", formatDelta(sorted))
	}
}

func TestSortChangeSet_Empty(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	t.Run("Empty ChangeSet", func(t *testing.T) {
		fromState := registry.State{}
		cs := registry.ChangeSet{}

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(sorted) != 0 {
			t.Errorf("expected empty result, got %d operations", len(sorted))
		}
	})

	t.Run("Nil FromState", func(t *testing.T) {
		cs := NewChangeSet().
			AddOp(createOp("test", "service")).
			Build()

		sorted, err := builder.SortChangeSet(nil, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(sorted) != 1 {
			t.Errorf("expected 1 operation, got %d", len(sorted))
		}
	})
}

func TestSortChangeSet_SingleOperationType(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	t.Run("Only Creates", func(t *testing.T) {
		fromState := registry.State{}
		cs := NewChangeSet().
			AddOp(createOp("test", "service").DependsOn("database")).
			AddOp(createOp("test", "database")).
			AddOp(createOp("test", "cache").DependsOn("database")).
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		verifyOperationOrder(t, sorted, []struct {
			operation       registry.Operation
			mustBeforeNames []string
			mustAfterNames  []string
		}{
			{
				operation:       createOp("test", "database").Build(),
				mustBeforeNames: []string{"service", "cache"},
			},
		})
	})

	t.Run("Only Updates", func(t *testing.T) {
		// Create initial state
		fromState := registry.State{
			testEntry{ns: "test", name: "database", kind: "service", data: "v1"}.toEntry(),
			testEntry{ns: "test", name: "service", kind: "service", data: "v1", dependsOn: []string{"database"}}.toEntry(),
		}

		cs := NewChangeSet().
			AddOp(updateOp("test", "service").Data("v2").DependsOn("database")).
			AddOp(updateOp("test", "database").Data("v2")).
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		verifyOperationOrder(t, sorted, []struct {
			operation       registry.Operation
			mustBeforeNames []string
			mustAfterNames  []string
		}{
			{
				operation:       updateOp("test", "database").Build(),
				mustBeforeNames: []string{"service"},
			},
		})
	})

	t.Run("Only Deletes", func(t *testing.T) {
		fromState := registry.State{
			testEntry{ns: "test", name: "database", kind: "service", data: "data"}.toEntry(),
			testEntry{ns: "test", name: "service", kind: "service", data: "data", dependsOn: []string{"database"}}.toEntry(),
			testEntry{ns: "test", name: "cache", kind: "service", data: "data", dependsOn: []string{"database"}}.toEntry(),
		}

		cs := NewChangeSet().
			AddOp(deleteOp("test", "service")).
			AddOp(deleteOp("test", "database")).
			AddOp(deleteOp("test", "cache")).
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// For deletes, dependents should be deleted before dependencies (reverse order)
		verifyOperationOrder(t, sorted, []struct {
			operation       registry.Operation
			mustBeforeNames []string
			mustAfterNames  []string
		}{
			{
				operation:       deleteOp("test", "service").Build(),
				mustBeforeNames: []string{"database"},
			},
			{
				operation:       deleteOp("test", "cache").Build(),
				mustBeforeNames: []string{"database"},
			},
		})
	})
}

func TestSortChangeSet_MixedOperations(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	t.Run("Mixed Without Dependencies", func(t *testing.T) {
		fromState := registry.State{
			testEntry{ns: "test", name: "existing", kind: "service", data: "v1"}.toEntry(),
		}

		cs := NewChangeSet().
			AddOp(updateOp("test", "existing").Data("v2")).
			AddOp(createOp("test", "new-service")).
			AddOp(deleteOp("test", "old-service")).
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(sorted) != 3 {
			t.Fatalf("expected 3 operations, got %d", len(sorted))
		}
	})

	t.Run("Mixed With Dependencies", func(t *testing.T) {
		fromState := registry.State{
			testEntry{ns: "test", name: "database", kind: "service", data: "v1"}.toEntry(),
			testEntry{ns: "test", name: "old-service", kind: "service", data: "v1", dependsOn: []string{"database"}}.toEntry(),
		}

		cs := NewChangeSet().
			AddOp(deleteOp("test", "old-service")).
			AddOp(updateOp("test", "database").Data("v2")).
			AddOp(createOp("test", "new-service").DependsOn("database")).
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		verifyOperationOrder(t, sorted, []struct {
			operation       registry.Operation
			mustBeforeNames []string
			mustAfterNames  []string
		}{
			{
				operation:       updateOp("test", "database").Build(),
				mustBeforeNames: []string{"new-service"},
			},
		})
	})
}

func TestSortChangeSet_Dependencies(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	t.Run("Simple Dependency Chain", func(t *testing.T) {
		fromState := registry.State{}

		cs := NewChangeSet().
			AddOp(createOp("test", "frontend").DependsOn("api")).
			AddOp(createOp("test", "api").DependsOn("database")).
			AddOp(createOp("test", "database")).
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		verifyOperationOrder(t, sorted, []struct {
			operation       registry.Operation
			mustBeforeNames []string
			mustAfterNames  []string
		}{
			{
				operation:       createOp("test", "database").Build(),
				mustBeforeNames: []string{"api", "frontend"},
			},
			{
				operation:       createOp("test", "api").Build(),
				mustBeforeNames: []string{"frontend"},
				mustAfterNames:  []string{"database"},
			},
		})
	})

	t.Run("Group Dependencies", func(t *testing.T) {
		fromState := registry.State{}

		cs := NewChangeSet().
			AddOp(createOp("test", "frontend").DependsOn("group:backend")).
			AddOp(createOp("test", "api").Groups("backend")).
			AddOp(createOp("test", "database").Groups("backend")).
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		verifyOperationOrder(t, sorted, []struct {
			operation       registry.Operation
			mustBeforeNames []string
			mustAfterNames  []string
		}{
			{
				operation:       createOp("test", "api").Build(),
				mustBeforeNames: []string{"frontend"},
			},
			{
				operation:       createOp("test", "database").Build(),
				mustBeforeNames: []string{"frontend"},
			},
		})
	})

	t.Run("Namespace Dependencies", func(t *testing.T) {
		fromState := registry.State{}

		cs := NewChangeSet().
			AddOp(createOp("app", "service").DependsOn("ns:infra")).
			AddOp(createOp("infra", "database")).
			AddOp(createOp("infra", "cache")).
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		verifyOperationOrder(t, sorted, []struct {
			operation       registry.Operation
			mustBeforeNames []string
			mustAfterNames  []string
		}{
			{
				operation:       createOp("infra", "database").Build(),
				mustBeforeNames: []string{"service"},
			},
			{
				operation:       createOp("infra", "cache").Build(),
				mustBeforeNames: []string{"service"},
			},
		})
	})
}

func TestSortChangeSet_EnvStorageDependencies(t *testing.T) {
	resolver := envResolver(t)
	builder := NewStateBuilder(zap.NewNop(), resolver)

	t.Run("create installs backing storages before router and variables", func(t *testing.T) {
		memory, osStorage, router, variable := envStorageGraph("app.test.env")

		changeSet := registry.ChangeSet{
			{Kind: registry.EntryCreate, Entry: variable},
			{Kind: registry.EntryCreate, Entry: router},
			{Kind: registry.EntryCreate, Entry: osStorage},
			{Kind: registry.EntryCreate, Entry: memory},
		}

		sorted, err := builder.SortChangeSet(nil, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertOperationBefore(t, sorted, registry.EntryCreate, memory.ID, registry.EntryCreate, router.ID)
		assertOperationBefore(t, sorted, registry.EntryCreate, osStorage.ID, registry.EntryCreate, router.ID)
		assertOperationBefore(t, sorted, registry.EntryCreate, router.ID, registry.EntryCreate, variable.ID)
		assertAppliesWithoutIncomingDependencyDelete(t, builder, nil, sorted)
	})

	t.Run("delete uninstalls variables before router and backing storages", func(t *testing.T) {
		memory, osStorage, router, variable := envStorageGraph("app.test.env")
		fromState := registry.State{variable, router, osStorage, memory}
		changeSet := registry.ChangeSet{
			{Kind: registry.EntryDelete, Entry: memory},
			{Kind: registry.EntryDelete, Entry: osStorage},
			{Kind: registry.EntryDelete, Entry: router},
			{Kind: registry.EntryDelete, Entry: variable},
		}

		sorted, err := builder.SortChangeSet(fromState, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertOperationBefore(t, sorted, registry.EntryDelete, variable.ID, registry.EntryDelete, router.ID)
		assertOperationBefore(t, sorted, registry.EntryDelete, router.ID, registry.EntryDelete, memory.ID)
		assertOperationBefore(t, sorted, registry.EntryDelete, router.ID, registry.EntryDelete, osStorage.ID)
		assertAppliesWithoutIncomingDependencyDelete(t, builder, fromState, sorted)
	})

	t.Run("rewire updates variable before deleting old router graph", func(t *testing.T) {
		oldMemory, oldOS, oldRouter, oldVariable := envStorageGraph("app.test.env.old")
		newMemory, newOS, newRouter, newVariable := envStorageGraph("app.test.env.new")
		newVariable.ID = oldVariable.ID

		fromState := registry.State{oldMemory, oldOS, oldRouter, oldVariable}
		changeSet := registry.ChangeSet{
			{Kind: registry.EntryDelete, Entry: oldMemory},
			{Kind: registry.EntryDelete, Entry: oldOS},
			{Kind: registry.EntryDelete, Entry: oldRouter},
			{Kind: registry.EntryUpdate, Entry: newVariable},
			{Kind: registry.EntryCreate, Entry: newRouter},
			{Kind: registry.EntryCreate, Entry: newOS},
			{Kind: registry.EntryCreate, Entry: newMemory},
		}

		sorted, err := builder.SortChangeSet(fromState, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertOperationBefore(t, sorted, registry.EntryCreate, newMemory.ID, registry.EntryCreate, newRouter.ID)
		assertOperationBefore(t, sorted, registry.EntryCreate, newOS.ID, registry.EntryCreate, newRouter.ID)
		assertOperationBefore(t, sorted, registry.EntryCreate, newRouter.ID, registry.EntryUpdate, oldVariable.ID)
		assertOperationBefore(t, sorted, registry.EntryUpdate, oldVariable.ID, registry.EntryDelete, oldRouter.ID)
		assertOperationBefore(t, sorted, registry.EntryDelete, oldRouter.ID, registry.EntryDelete, oldMemory.ID)
		assertOperationBefore(t, sorted, registry.EntryDelete, oldRouter.ID, registry.EntryDelete, oldOS.ID)
		assertAppliesWithoutIncomingDependencyDelete(t, builder, fromState, sorted)
	})
}

func TestSortChangeSet_ComplexScenarios(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	t.Run("Complex Dependency Tree", func(t *testing.T) {
		fromState := registry.State{
			testEntry{ns: "infra", name: "database", kind: "service", data: "v1", groups: []string{"storage"}}.toEntry(),
			testEntry{ns: "app", name: "old-api", kind: "service", data: "v1", dependsOn: []string{"group:storage"}}.toEntry(),
		}

		cs := NewChangeSet().
			// Delete old API (should be first)
			AddOp(deleteOp("app", "old-api")).
			// Update database (dependency)
			AddOp(updateOp("infra", "database").Data("v2").Groups("storage")).
			// Create new services that depend on storage
			AddOp(createOp("app", "new-api").DependsOn("group:storage")).
			AddOp(createOp("infra", "cache").Groups("storage")).
			// Create frontend that depends on new API
			AddOp(createOp("web", "frontend").DependsOn("app:new-api")).
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		verifyOperationOrder(t, sorted, []struct {
			operation       registry.Operation
			mustBeforeNames []string
			mustAfterNames  []string
		}{
			// Storage components before their dependents
			{
				operation:       updateOp("infra", "database").Build(),
				mustBeforeNames: []string{"new-api", "frontend"},
			},
			{
				operation:       createOp("infra", "cache").Build(),
				mustBeforeNames: []string{"new-api", "frontend"},
			},
			// API before frontend
			{
				operation:       createOp("app", "new-api").Build(),
				mustBeforeNames: []string{"frontend"},
			},
		})
	})

	t.Run("Multiple Namespaces and Groups", func(t *testing.T) {
		fromState := registry.State{}

		cs := NewChangeSet().
			// Web tier
			AddOp(createOp("web", "frontend").DependsOn("ns:app")).
			AddOp(createOp("web", "cdn").Groups("web-tier")).
			// App tier
			AddOp(createOp("app", "api").DependsOn("ns:infra", "group:web-tier")).
			AddOp(createOp("app", "auth").DependsOn("ns:infra")).
			// Infrastructure
			AddOp(createOp("infra", "database").Groups("persistence")).
			AddOp(createOp("infra", "cache").Groups("persistence")).
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		verifyOperationOrder(t, sorted, []struct {
			operation       registry.Operation
			mustBeforeNames []string
			mustAfterNames  []string
		}{
			// Infrastructure first
			{
				operation:       createOp("infra", "database").Build(),
				mustBeforeNames: []string{"api", "auth", "frontend"},
			},
			{
				operation:       createOp("infra", "cache").Build(),
				mustBeforeNames: []string{"api", "auth", "frontend"},
			},
			// Web tier before app (app depends on web-tier group)
			{
				operation:       createOp("web", "cdn").Build(),
				mustBeforeNames: []string{"api"},
			},
			// App tier before web frontend
			{
				operation:       createOp("app", "api").Build(),
				mustBeforeNames: []string{"frontend"},
			},
			{
				operation:       createOp("app", "auth").Build(),
				mustBeforeNames: []string{"frontend"},
			},
		})
	})
}

func TestSortChangeSet_RuntimeImportRewire(t *testing.T) {
	resolver := importResolver(t)
	builder := NewStateBuilder(zap.NewNop(), resolver)

	t.Run("helper namespace move updates importer before deleting old helpers", func(t *testing.T) {
		oldNS := "keeper.agents.tools._internal.flow"
		newNS := "keeper.internal.flow"

		oldRender := importEntry(oldNS, "render", "library.lua", "old render", nil)
		oldRepo := importEntry(oldNS, "repo", "library.lua", "old repo", nil)
		oldDetectors := importEntry(oldNS, "detectors", "library.lua", "old detectors", map[string]string{
			"render": oldRender.ID.String(),
			"repo":   oldRepo.ID.String(),
		})
		oldDataflow := importEntry("keeper.agents.tools", "dataflow", "function.lua", "old dataflow", map[string]string{
			"render":    oldRender.ID.String(),
			"repo":      oldRepo.ID.String(),
			"detectors": oldDetectors.ID.String(),
		})

		newRender := importEntry(newNS, "render", "library.lua", "new render", nil)
		newRepo := importEntry(newNS, "repo", "library.lua", "new repo", nil)
		newDetectors := importEntry(newNS, "detectors", "library.lua", "new detectors", map[string]string{
			"render": newRender.ID.String(),
			"repo":   newRepo.ID.String(),
		})
		newDataflow := importEntry("keeper.agents.tools", "dataflow", "function.lua", "new dataflow", map[string]string{
			"render":    newRender.ID.String(),
			"repo":      newRepo.ID.String(),
			"detectors": newDetectors.ID.String(),
		})

		fromState := registry.State{oldDataflow, oldDetectors, oldRender, oldRepo}
		changeSet := registry.ChangeSet{
			{Kind: registry.EntryDelete, Entry: oldRender},
			{Kind: registry.EntryDelete, Entry: oldRepo},
			{Kind: registry.EntryDelete, Entry: oldDetectors},
			{Kind: registry.EntryUpdate, Entry: newDataflow},
			{Kind: registry.EntryCreate, Entry: newDetectors},
			{Kind: registry.EntryCreate, Entry: newRepo},
			{Kind: registry.EntryCreate, Entry: newRender},
		}

		sorted, err := builder.SortChangeSet(fromState, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertOperationBefore(t, sorted, registry.EntryCreate, newRender.ID, registry.EntryCreate, newDetectors.ID)
		assertOperationBefore(t, sorted, registry.EntryCreate, newRepo.ID, registry.EntryCreate, newDetectors.ID)
		assertOperationBefore(t, sorted, registry.EntryCreate, newDetectors.ID, registry.EntryUpdate, newDataflow.ID)
		assertOperationBefore(t, sorted, registry.EntryUpdate, newDataflow.ID, registry.EntryDelete, oldDetectors.ID)
		assertOperationBefore(t, sorted, registry.EntryUpdate, newDataflow.ID, registry.EntryDelete, oldRender.ID)
		assertOperationBefore(t, sorted, registry.EntryUpdate, newDataflow.ID, registry.EntryDelete, oldRepo.ID)
		assertOperationBefore(t, sorted, registry.EntryDelete, oldDetectors.ID, registry.EntryDelete, oldRender.ID)
		assertOperationBefore(t, sorted, registry.EntryDelete, oldDetectors.ID, registry.EntryDelete, oldRepo.ID)
		assertAppliesWithoutIncomingDependencyDelete(t, builder, fromState, sorted)
	})

	t.Run("build delta output is repaired before live apply", func(t *testing.T) {
		oldHelper := importEntry("old.lib", "helper", "library.lua", "old helper", nil)
		oldConsumer := importEntry("app", "consumer", "function.lua", "old consumer", map[string]string{
			"helper": oldHelper.ID.String(),
		})
		newHelper := importEntry("new.lib", "helper", "library.lua", "new helper", nil)
		newConsumer := importEntry("app", "consumer", "function.lua", "new consumer", map[string]string{
			"helper": newHelper.ID.String(),
		})

		fromState := registry.State{oldConsumer, oldHelper}
		toState := registry.State{newConsumer, newHelper}
		delta, err := builder.BuildDelta(fromState, toState)
		if err != nil {
			t.Fatalf("build delta: %v", err)
		}

		assertOperationBefore(t, delta, registry.EntryCreate, newHelper.ID, registry.EntryUpdate, newConsumer.ID)
		assertOperationBefore(t, delta, registry.EntryUpdate, newConsumer.ID, registry.EntryDelete, oldHelper.ID)
		assertAppliesWithoutIncomingDependencyDelete(t, builder, fromState, delta)
	})

	t.Run("all live dependents move or delete before shared helper delete", func(t *testing.T) {
		oldShared := importEntry("old.shared", "tool", "library.lua", "old shared", nil)
		newShared := importEntry("new.shared", "tool", "library.lua", "new shared", nil)
		consumerA := importEntry("app", "consumer_a", "function.lua", "a old", map[string]string{
			"shared": oldShared.ID.String(),
		})
		consumerB := importEntry("app", "consumer_b", "function.lua", "b old", map[string]string{
			"shared": oldShared.ID.String(),
		})
		consumerC := importEntry("app", "consumer_c", "function.lua", "c old", map[string]string{
			"shared": oldShared.ID.String(),
		})
		consumerANew := importEntry("app", "consumer_a", "function.lua", "a new", map[string]string{
			"shared": newShared.ID.String(),
		})
		consumerCNew := importEntry("app", "consumer_c", "function.lua", "c no longer imports shared", nil)

		changeSet := registry.ChangeSet{
			{Kind: registry.EntryDelete, Entry: oldShared},
			{Kind: registry.EntryDelete, Entry: consumerB},
			{Kind: registry.EntryUpdate, Entry: consumerCNew},
			{Kind: registry.EntryUpdate, Entry: consumerANew},
			{Kind: registry.EntryCreate, Entry: newShared},
		}

		sorted, err := builder.SortChangeSet(registry.State{oldShared, consumerA, consumerB, consumerC}, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertOperationBefore(t, sorted, registry.EntryCreate, newShared.ID, registry.EntryUpdate, consumerANew.ID)
		assertOperationBefore(t, sorted, registry.EntryUpdate, consumerANew.ID, registry.EntryDelete, oldShared.ID)
		assertOperationBefore(t, sorted, registry.EntryDelete, consumerB.ID, registry.EntryDelete, oldShared.ID)
		assertOperationBefore(t, sorted, registry.EntryUpdate, consumerCNew.ID, registry.EntryDelete, oldShared.ID)
		assertAppliesWithoutIncomingDependencyDelete(t, builder, registry.State{oldShared, consumerA, consumerB, consumerC}, sorted)
	})

	t.Run("delete-only import graph removes importers before helpers", func(t *testing.T) {
		helper := importEntry("lib", "helper", "library.lua", "helper", nil)
		adapter := importEntry("lib", "adapter", "library.lua", "adapter", map[string]string{
			"helper": helper.ID.String(),
		})
		consumer := importEntry("app", "consumer", "function.lua", "consumer", map[string]string{
			"adapter": adapter.ID.String(),
			"helper":  helper.ID.String(),
		})

		changeSet := registry.ChangeSet{
			{Kind: registry.EntryDelete, Entry: helper},
			{Kind: registry.EntryDelete, Entry: adapter},
			{Kind: registry.EntryDelete, Entry: consumer},
		}

		sorted, err := builder.SortChangeSet(registry.State{helper, adapter, consumer}, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertOperationBefore(t, sorted, registry.EntryDelete, consumer.ID, registry.EntryDelete, adapter.ID)
		assertOperationBefore(t, sorted, registry.EntryDelete, adapter.ID, registry.EntryDelete, helper.ID)
		assertOperationBefore(t, sorted, registry.EntryDelete, consumer.ID, registry.EntryDelete, helper.ID)
		assertAppliesWithoutIncomingDependencyDelete(t, builder, registry.State{helper, adapter, consumer}, sorted)
	})

	t.Run("partial rewire leaves old helper delete visibly invalid", func(t *testing.T) {
		oldShared := importEntry("old.shared", "tool", "library.lua", "old shared", nil)
		newShared := importEntry("new.shared", "tool", "library.lua", "new shared", nil)
		consumerA := importEntry("app", "consumer_a", "function.lua", "a old", map[string]string{
			"shared": oldShared.ID.String(),
		})
		consumerB := importEntry("app", "consumer_b", "function.lua", "b still old", map[string]string{
			"shared": oldShared.ID.String(),
		})
		consumerANew := importEntry("app", "consumer_a", "function.lua", "a new", map[string]string{
			"shared": newShared.ID.String(),
		})

		changeSet := registry.ChangeSet{
			{Kind: registry.EntryDelete, Entry: oldShared},
			{Kind: registry.EntryUpdate, Entry: consumerANew},
			{Kind: registry.EntryCreate, Entry: newShared},
		}

		sorted, err := builder.SortChangeSet(registry.State{oldShared, consumerA, consumerB}, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertOperationBefore(t, sorted, registry.EntryCreate, newShared.ID, registry.EntryUpdate, consumerANew.ID)
		assertOperationBefore(t, sorted, registry.EntryUpdate, consumerANew.ID, registry.EntryDelete, oldShared.ID)
		assertCannotApplyWithoutIncomingDependencyDelete(t, builder, registry.State{oldShared, consumerA, consumerB}, sorted)
	})

	t.Run("dependent update that still imports deleted helper remains invalid", func(t *testing.T) {
		helper := importEntry("lib", "helper", "library.lua", "helper", nil)
		consumer := importEntry("app", "consumer", "function.lua", "old consumer", map[string]string{
			"helper": helper.ID.String(),
		})
		consumerStillUsingHelper := importEntry("app", "consumer", "function.lua", "new source same import", map[string]string{
			"helper": helper.ID.String(),
		})

		changeSet := registry.ChangeSet{
			{Kind: registry.EntryDelete, Entry: helper},
			{Kind: registry.EntryUpdate, Entry: consumerStillUsingHelper},
		}

		sorted, err := builder.SortChangeSet(registry.State{helper, consumer}, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertOperationBefore(t, sorted, registry.EntryUpdate, consumer.ID, registry.EntryDelete, helper.ID)
		assertCannotApplyWithoutIncomingDependencyDelete(t, builder, registry.State{helper, consumer}, sorted)
	})

	t.Run("duplicate meta and resolver dependencies are coalesced", func(t *testing.T) {
		helper := importEntry("lib", "helper", "library.lua", "helper", nil)
		consumer := importEntry("app", "consumer", "function.lua", "consumer", map[string]string{
			"helper": helper.ID.String(),
		})
		consumer.Meta[registry.TagDependsOn] = []string{helper.ID.String()}

		changeSet := registry.ChangeSet{
			{Kind: registry.EntryCreate, Entry: consumer},
			{Kind: registry.EntryCreate, Entry: helper},
		}

		sorted, err := builder.SortChangeSet(nil, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertOperationBefore(t, sorted, registry.EntryCreate, helper.ID, registry.EntryCreate, consumer.ID)
		assertAppliesWithoutIncomingDependencyDelete(t, builder, nil, sorted)
	})

	t.Run("update can depend on existing unchanged import target", func(t *testing.T) {
		helper := importEntry("lib", "helper", "library.lua", "helper", nil)
		consumer := importEntry("app", "consumer", "function.lua", "old consumer", map[string]string{
			"helper": helper.ID.String(),
		})
		consumerUpdated := importEntry("app", "consumer", "function.lua", "new source same valid import", map[string]string{
			"helper": helper.ID.String(),
		})

		changeSet := registry.ChangeSet{{Kind: registry.EntryUpdate, Entry: consumerUpdated}}

		sorted, err := builder.SortChangeSet(registry.State{helper, consumer}, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sorted) != 1 || sorted[0].Kind != registry.EntryUpdate || !sorted[0].Entry.ID.Equal(consumer.ID) {
			t.Fatalf("unexpected sorted update-only changeset:\n%s", formatDelta(sorted))
		}
		assertAppliesWithoutIncomingDependencyDelete(t, builder, registry.State{helper, consumer}, sorted)
	})
}

func TestSortChangeSet_RewireMetaDependencies(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	t.Run("namespace dependency move updates dependent before deleting old namespace", func(t *testing.T) {
		oldDB := testEntry{ns: "old.infra", name: "db", kind: "service", data: "old-db"}.toEntry()
		oldCache := testEntry{ns: "old.infra", name: "cache", kind: "service", data: "old-cache"}.toEntry()
		newDB := testEntry{ns: "new.infra", name: "db", kind: "service", data: "new-db"}.toEntry()
		newCache := testEntry{ns: "new.infra", name: "cache", kind: "service", data: "new-cache"}.toEntry()
		appOld := testEntry{ns: "app", name: "service", kind: "service", data: "old", dependsOn: []string{"ns:old.infra"}}.toEntry()
		appNew := testEntry{ns: "app", name: "service", kind: "service", data: "new", dependsOn: []string{"ns:new.infra"}}.toEntry()

		changeSet := registry.ChangeSet{
			{Kind: registry.EntryDelete, Entry: oldDB},
			{Kind: registry.EntryDelete, Entry: oldCache},
			{Kind: registry.EntryUpdate, Entry: appNew},
			{Kind: registry.EntryCreate, Entry: newCache},
			{Kind: registry.EntryCreate, Entry: newDB},
		}

		sorted, err := builder.SortChangeSet(registry.State{oldDB, oldCache, appOld}, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertOperationBefore(t, sorted, registry.EntryCreate, newDB.ID, registry.EntryUpdate, appNew.ID)
		assertOperationBefore(t, sorted, registry.EntryCreate, newCache.ID, registry.EntryUpdate, appNew.ID)
		assertOperationBefore(t, sorted, registry.EntryUpdate, appNew.ID, registry.EntryDelete, oldDB.ID)
		assertOperationBefore(t, sorted, registry.EntryUpdate, appNew.ID, registry.EntryDelete, oldCache.ID)
		assertAppliesWithoutIncomingDependencyDelete(t, builder, registry.State{oldDB, oldCache, appOld}, sorted)
	})

	t.Run("group dependency move updates dependent before deleting old group members", func(t *testing.T) {
		oldDB := testEntry{ns: "infra", name: "old_db", kind: "service", data: "old-db", groups: []string{"storage-old"}}.toEntry()
		oldCache := testEntry{ns: "infra", name: "old_cache", kind: "service", data: "old-cache", groups: []string{"storage-old"}}.toEntry()
		newDB := testEntry{ns: "infra", name: "new_db", kind: "service", data: "new-db", groups: []string{"storage-new"}}.toEntry()
		newCache := testEntry{ns: "infra", name: "new_cache", kind: "service", data: "new-cache", groups: []string{"storage-new"}}.toEntry()
		appOld := testEntry{ns: "app", name: "service", kind: "service", data: "old", dependsOn: []string{"group:storage-old"}}.toEntry()
		appNew := testEntry{ns: "app", name: "service", kind: "service", data: "new", dependsOn: []string{"group:storage-new"}}.toEntry()

		changeSet := registry.ChangeSet{
			{Kind: registry.EntryDelete, Entry: oldDB},
			{Kind: registry.EntryDelete, Entry: oldCache},
			{Kind: registry.EntryUpdate, Entry: appNew},
			{Kind: registry.EntryCreate, Entry: newCache},
			{Kind: registry.EntryCreate, Entry: newDB},
		}

		sorted, err := builder.SortChangeSet(registry.State{oldDB, oldCache, appOld}, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertOperationBefore(t, sorted, registry.EntryCreate, newDB.ID, registry.EntryUpdate, appNew.ID)
		assertOperationBefore(t, sorted, registry.EntryCreate, newCache.ID, registry.EntryUpdate, appNew.ID)
		assertOperationBefore(t, sorted, registry.EntryUpdate, appNew.ID, registry.EntryDelete, oldDB.ID)
		assertOperationBefore(t, sorted, registry.EntryUpdate, appNew.ID, registry.EntryDelete, oldCache.ID)
		assertAppliesWithoutIncomingDependencyDelete(t, builder, registry.State{oldDB, oldCache, appOld}, sorted)
	})

	t.Run("partial group cleanup remains visibly invalid while dependent uses group", func(t *testing.T) {
		oldDB := testEntry{ns: "infra", name: "old_db", kind: "service", data: "old-db", groups: []string{"storage-old"}}.toEntry()
		oldCache := testEntry{ns: "infra", name: "old_cache", kind: "service", data: "old-cache", groups: []string{"storage-old"}}.toEntry()
		app := testEntry{ns: "app", name: "service", kind: "service", data: "app", dependsOn: []string{"group:storage-old"}}.toEntry()

		changeSet := registry.ChangeSet{{Kind: registry.EntryDelete, Entry: oldDB}}

		sorted, err := builder.SortChangeSet(registry.State{oldDB, oldCache, app}, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertCannotApplyWithoutIncomingDependencyDelete(t, builder, registry.State{oldDB, oldCache, app}, sorted)
	})

	t.Run("same id replacement deletes old entry before recreating it", func(t *testing.T) {
		oldHandler := testEntry{ns: "app", name: "handler", kind: "function.lua", data: "old"}.toEntry()
		newHandler := testEntry{ns: "app", name: "handler", kind: "library.lua", data: "new"}.toEntry()
		endpoint := testEntry{ns: "app", name: "endpoint", kind: "http.endpoint", data: "endpoint", dependsOn: []string{"app:handler"}}.toEntry()

		changeSet := registry.ChangeSet{
			{Kind: registry.EntryCreate, Entry: endpoint},
			{Kind: registry.EntryCreate, Entry: newHandler},
			{Kind: registry.EntryDelete, Entry: oldHandler},
		}

		sorted, err := builder.SortChangeSet(registry.State{oldHandler}, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertOperationBefore(t, sorted, registry.EntryDelete, oldHandler.ID, registry.EntryCreate, newHandler.ID)
		assertOperationBefore(t, sorted, registry.EntryCreate, newHandler.ID, registry.EntryCreate, endpoint.ID)
		assertAppliesWithoutIncomingDependencyDelete(t, builder, registry.State{oldHandler}, sorted)
	})

	t.Run("same id replacement with deleted live dependent is ordered safely", func(t *testing.T) {
		oldHandler := testEntry{ns: "app", name: "handler", kind: "function.lua", data: "old"}.toEntry()
		newHandler := testEntry{ns: "app", name: "handler", kind: "library.lua", data: "new"}.toEntry()
		endpoint := testEntry{ns: "app", name: "endpoint", kind: "http.endpoint", data: "endpoint", dependsOn: []string{"app:handler"}}.toEntry()

		changeSet := registry.ChangeSet{
			{Kind: registry.EntryCreate, Entry: newHandler},
			{Kind: registry.EntryDelete, Entry: oldHandler},
			{Kind: registry.EntryDelete, Entry: endpoint},
		}

		sorted, err := builder.SortChangeSet(registry.State{oldHandler, endpoint}, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertOperationBefore(t, sorted, registry.EntryDelete, endpoint.ID, registry.EntryDelete, oldHandler.ID)
		assertOperationBefore(t, sorted, registry.EntryDelete, oldHandler.ID, registry.EntryCreate, newHandler.ID)
		assertAppliesWithoutIncomingDependencyDelete(t, builder, registry.State{oldHandler, endpoint}, sorted)
	})

	t.Run("same id replacement with live dependent rewired away is ordered safely", func(t *testing.T) {
		oldHandler := testEntry{ns: "app", name: "handler", kind: "function.lua", data: "old"}.toEntry()
		newHandler := testEntry{ns: "app", name: "handler", kind: "library.lua", data: "new"}.toEntry()
		otherHandler := testEntry{ns: "app", name: "other_handler", kind: "function.lua", data: "other"}.toEntry()
		endpoint := testEntry{ns: "app", name: "endpoint", kind: "http.endpoint", data: "endpoint", dependsOn: []string{"app:handler"}}.toEntry()
		endpointRewired := testEntry{ns: "app", name: "endpoint", kind: "http.endpoint", data: "endpoint", dependsOn: []string{"app:other_handler"}}.toEntry()

		changeSet := registry.ChangeSet{
			{Kind: registry.EntryCreate, Entry: newHandler},
			{Kind: registry.EntryDelete, Entry: oldHandler},
			{Kind: registry.EntryUpdate, Entry: endpointRewired},
			{Kind: registry.EntryCreate, Entry: otherHandler},
		}

		sorted, err := builder.SortChangeSet(registry.State{oldHandler, endpoint}, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertOperationBefore(t, sorted, registry.EntryCreate, otherHandler.ID, registry.EntryUpdate, endpoint.ID)
		assertOperationBefore(t, sorted, registry.EntryUpdate, endpoint.ID, registry.EntryDelete, oldHandler.ID)
		assertOperationBefore(t, sorted, registry.EntryDelete, oldHandler.ID, registry.EntryCreate, newHandler.ID)
		assertAppliesWithoutIncomingDependencyDelete(t, builder, registry.State{oldHandler, endpoint}, sorted)
	})

	t.Run("same id replacement with unchanged live dependent remains invalid", func(t *testing.T) {
		oldHandler := testEntry{ns: "app", name: "handler", kind: "function.lua", data: "old"}.toEntry()
		newHandler := testEntry{ns: "app", name: "handler", kind: "library.lua", data: "new"}.toEntry()
		endpoint := testEntry{ns: "app", name: "endpoint", kind: "http.endpoint", data: "endpoint", dependsOn: []string{"app:handler"}}.toEntry()

		changeSet := registry.ChangeSet{
			{Kind: registry.EntryCreate, Entry: newHandler},
			{Kind: registry.EntryDelete, Entry: oldHandler},
		}

		sorted, err := builder.SortChangeSet(registry.State{oldHandler, endpoint}, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertOperationBefore(t, sorted, registry.EntryDelete, oldHandler.ID, registry.EntryCreate, newHandler.ID)
		assertCannotApplyWithoutIncomingDependencyDelete(t, builder, registry.State{oldHandler, endpoint}, sorted)
	})

	t.Run("same id replacement cannot be repaired by dependent update to same id", func(t *testing.T) {
		oldHandler := testEntry{ns: "app", name: "handler", kind: "function.lua", data: "old"}.toEntry()
		newHandler := testEntry{ns: "app", name: "handler", kind: "library.lua", data: "new"}.toEntry()
		endpoint := testEntry{ns: "app", name: "endpoint", kind: "http.endpoint", data: "endpoint", dependsOn: []string{"app:handler"}}.toEntry()
		endpointStillDependingOnHandler := testEntry{ns: "app", name: "endpoint", kind: "http.endpoint", data: "new endpoint", dependsOn: []string{"app:handler"}}.toEntry()

		changeSet := registry.ChangeSet{
			{Kind: registry.EntryCreate, Entry: newHandler},
			{Kind: registry.EntryUpdate, Entry: endpointStillDependingOnHandler},
			{Kind: registry.EntryDelete, Entry: oldHandler},
		}

		sorted, err := builder.SortChangeSet(registry.State{oldHandler, endpoint}, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertCannotApplyWithoutIncomingDependencyDelete(t, builder, registry.State{oldHandler, endpoint}, sorted)
	})

	t.Run("missing dependent operation remains visibly invalid", func(t *testing.T) {
		helper := testEntry{ns: "lib", name: "helper", kind: "service", data: "helper"}.toEntry()
		consumer := testEntry{ns: "app", name: "consumer", kind: "service", data: "consumer", dependsOn: []string{"lib:helper"}}.toEntry()
		changeSet := registry.ChangeSet{{Kind: registry.EntryDelete, Entry: helper}}

		sorted, err := builder.SortChangeSet(registry.State{helper, consumer}, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sorted) != 1 || sorted[0].Kind != registry.EntryDelete || !sorted[0].Entry.ID.Equal(helper.ID) {
			t.Fatalf("sorter should not invent repair operations for an invalid delete-only changeset: %s", formatDelta(sorted))
		}
	})
}

func TestSortChangeSet_CircularDependencies(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	t.Run("Simple Circular Dependency", func(t *testing.T) {
		fromState := registry.State{}

		cs := NewChangeSet().
			AddOp(createOp("test", "service-a").DependsOn("service-b")).
			AddOp(createOp("test", "service-b").DependsOn("service-a")).
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)

		// Should handle circular dependencies gracefully (fallback to lexicographical or detect cycle)
		if err != nil {
			// If cycle detection is implemented, error is expected
			if !strings.Contains(err.Error(), "cycle detected") {
				t.Errorf("expected 'cycle detected' error, got: %v", err)
			}
		} else {
			// If fallback to lexicographical sort, should still have both operations
			if len(sorted) != 2 {
				t.Errorf("expected 2 operations despite circular dependency, got %d", len(sorted))
			}
		}
	})

	t.Run("Complex Circular Through Groups", func(t *testing.T) {
		fromState := registry.State{}

		cs := NewChangeSet().
			AddOp(createOp("test", "service-a").Groups("group-a").DependsOn("group:group-b")).
			AddOp(createOp("test", "service-b").Groups("group-b").DependsOn("group:group-a")).
			Build()

		_, err := builder.SortChangeSet(fromState, cs)

		// Should handle circular dependencies through groups
		if err != nil && !strings.Contains(err.Error(), "cycle detected") {
			t.Errorf("unexpected error type: %v", err)
		}
	})
}

func TestSortChangeSet_RealWorldScenarios(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	t.Run("Microservice Deployment", func(t *testing.T) {
		fromState := registry.State{
			// Existing infrastructure
			testEntry{ns: "infra", name: "database", kind: "service", data: "v1"}.toEntry(),
			testEntry{ns: "infra", name: "old-cache", kind: "service", data: "v1"}.toEntry(),
			// Existing services
			testEntry{ns: "app", name: "user-service", kind: "service", data: "v1", dependsOn: []string{"infra:database"}}.toEntry(),
		}

		cs := NewChangeSet().
			// Remove old cache
			AddOp(deleteOp("infra", "old-cache")).
			// Update database
			AddOp(updateOp("infra", "database").Data("v2")).
			// Update existing service
			AddOp(updateOp("app", "user-service").Data("v2").DependsOn("infra:database")).
			// Add new cache
			AddOp(createOp("infra", "redis-cache").Kind("cache")).
			// Add new services
			AddOp(createOp("app", "auth-service").DependsOn("infra:database")).
			AddOp(createOp("app", "api-gateway").DependsOn("app:user-service", "app:auth-service")).
			// Add monitoring
			AddOp(createOp("monitoring", "metrics").DependsOn("ns:app")).
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		verifyOperationOrder(t, sorted, []struct {
			operation       registry.Operation
			mustBeforeNames []string
			mustAfterNames  []string
		}{
			// Infrastructure updates before services
			{
				operation:       updateOp("infra", "database").Build(),
				mustBeforeNames: []string{"user-service", "auth-service", "api-gateway", "metrics"},
			},
			// Services before API gateway
			{
				operation:       updateOp("app", "user-service").Build(),
				mustBeforeNames: []string{"api-gateway", "metrics"},
			},
			{
				operation:       createOp("app", "auth-service").Build(),
				mustBeforeNames: []string{"api-gateway", "metrics"},
			},
			// Gateway before monitoring
			{
				operation:       createOp("app", "api-gateway").Build(),
				mustBeforeNames: []string{"metrics"},
			},
		})
	})

	t.Run("Database Migration Scenario", func(t *testing.T) {
		fromState := registry.State{
			testEntry{ns: "data", name: "old-db", kind: "database", data: "mysql"}.toEntry(),
			testEntry{ns: "app", name: "service-a", kind: "service", data: "v1", dependsOn: []string{"data:old-db"}}.toEntry(),
			testEntry{ns: "app", name: "service-b", kind: "service", data: "v1", dependsOn: []string{"data:old-db"}}.toEntry(),
		}

		cs := NewChangeSet().
			// Create new database
			AddOp(createOp("data", "new-db").Kind("database").Data("postgres")).
			// Update services to use new database
			AddOp(updateOp("app", "service-a").Data("v2").DependsOn("data:new-db")).
			AddOp(updateOp("app", "service-b").Data("v2").DependsOn("data:new-db")).
			// Remove old database (after services are updated)
			AddOp(deleteOp("data", "old-db")).
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		verifyOperationOrder(t, sorted, []struct {
			operation       registry.Operation
			mustBeforeNames []string
			mustAfterNames  []string
		}{
			{
				operation:       createOp("data", "new-db").Build(),
				mustBeforeNames: []string{"service-a", "service-b"},
			},
			{
				operation:       updateOp("app", "service-a").Build(),
				mustBeforeNames: []string{"old-db"},
				mustAfterNames:  []string{"new-db"},
			},
			{
				operation:       updateOp("app", "service-b").Build(),
				mustBeforeNames: []string{"old-db"},
				mustAfterNames:  []string{"new-db"},
			},
		})
	})
}

func TestSortChangeSet_EdgeCases(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	t.Run("Operations With No Dependencies", func(t *testing.T) {
		fromState := registry.State{}

		cs := NewChangeSet().
			AddOp(createOp("test", "service-c")).
			AddOp(createOp("test", "service-a")).
			AddOp(createOp("test", "service-b")).
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should maintain some deterministic order (likely lexicographical)
		if len(sorted) != 3 {
			t.Errorf("expected 3 operations, got %d", len(sorted))
		}
	})

	t.Run("Unconstrained Operations Sort Lexicographically", func(t *testing.T) {
		// SortChangeSet normalizes its input by (entry.ID.NS, entry.ID.Name, kind)
		// before computing topological constraints. For operations with no
		// dependency edges between them, this normalization is what the caller
		// observes: the output is sorted lexicographically by ID, with kind as
		// the final tie-breaker. This is the contract that lets upstream code
		// pass a Go-map-iterated slice (random hash-seed order) without the
		// randomness leaking into the registry state.
		fromState := registry.State{
			testEntry{ns: "app", name: "old-a", kind: "service", data: "old-a"}.toEntry(),
			testEntry{ns: "app", name: "old-b", kind: "service", data: "old-b"}.toEntry(),
		}
		changeSet := registry.ChangeSet{
			createOp("app", "new-c").Build(),
			updateOp("app", "old-b").Data("new-b").Build(),
			createOp("app", "new-a").Build(),
			deleteOp("app", "old-a").Build(),
		}

		sorted, err := builder.SortChangeSet(fromState, changeSet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expected := []struct {
			kind string
			name string
		}{
			{registry.EntryCreate, "new-a"},
			{registry.EntryCreate, "new-c"},
			{registry.EntryDelete, "old-a"},
			{registry.EntryUpdate, "old-b"},
		}
		if len(sorted) != len(expected) {
			t.Fatalf("expected %d operations, got %d\nsorted:\n%s", len(expected), len(sorted), formatDelta(sorted))
		}
		for i, want := range expected {
			if sorted[i].Kind != want.kind || sorted[i].Entry.ID.Name != want.name {
				t.Fatalf("operation %d wrong: got kind=%s name=%s, want kind=%s name=%s\nsorted:\n%s",
					i, sorted[i].Kind, sorted[i].Entry.ID.Name, want.kind, want.name, formatDelta(sorted))
			}
		}
		assertAppliesWithoutIncomingDependencyDelete(t, builder, fromState, sorted)
	})

	t.Run("Dependencies On Non-Existent Entries", func(t *testing.T) {
		fromState := registry.State{}

		cs := NewChangeSet().
			AddOp(createOp("test", "service").DependsOn("non-existent")).
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should handle gracefully - dependency on non-existent entry is ignored
		if len(sorted) != 1 {
			t.Errorf("expected 1 operation, got %d", len(sorted))
		}
	})

	t.Run("Self Dependencies", func(t *testing.T) {
		fromState := registry.State{}

		cs := NewChangeSet().
			AddOp(createOp("test", "service").DependsOn("service")). // Self-dependency
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		// Should handle gracefully (cycle detection will trigger fallback)
		if err != nil {
			t.Logf("Cycle detected as expected: %v", err)
		}

		// Should still have the operation
		if len(sorted) != 1 {
			t.Errorf("expected 1 operation, got %d", len(sorted))
		}
	})
}

func TestSortChangeSet_WrongOrder(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop(), nil)

	t.Run("Creates in Wrong Dependency Order", func(t *testing.T) {
		fromState := registry.State{}

		// Intentionally provide operations in wrong order
		cs := NewChangeSet().
			AddOp(createOp("test", "frontend").DependsOn("api")). // Dependent first
			AddOp(createOp("test", "ui").DependsOn("frontend")).  // Deep dependent second
			AddOp(createOp("test", "api").DependsOn("database")). // Mid-dependency third
			AddOp(createOp("test", "database")).                  // Root dependency last
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should fix the order to: database -> api -> frontend -> ui
		verifyOperationOrder(t, sorted, []struct {
			operation       registry.Operation
			mustBeforeNames []string
			mustAfterNames  []string
		}{
			{
				operation:       createOp("test", "database").Build(),
				mustBeforeNames: []string{"api", "frontend", "ui"},
			},
			{
				operation:       createOp("test", "api").Build(),
				mustBeforeNames: []string{"frontend", "ui"},
				mustAfterNames:  []string{"database"},
			},
			{
				operation:       createOp("test", "frontend").Build(),
				mustBeforeNames: []string{"ui"},
				mustAfterNames:  []string{"database", "api"},
			},
		})
	})

	t.Run("Mixed Operations in Wrong Order", func(t *testing.T) {
		fromState := registry.State{
			testEntry{ns: "test", name: "old-api", kind: "service", data: "v1", dependsOn: []string{"database"}}.toEntry(),
			testEntry{ns: "test", name: "database", kind: "service", data: "v1"}.toEntry(),
		}

		// Provide operations in completely wrong order
		cs := NewChangeSet().
			AddOp(createOp("test", "new-frontend").DependsOn("new-api")). // Create dependent first
			AddOp(updateOp("test", "database").Data("v2")).               // Update in middle
			AddOp(createOp("test", "new-api").DependsOn("database")).     // Create dependency after dependent
			AddOp(deleteOp("test", "old-api")).
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should fix target-side order to: update database -> create new-api -> create new-frontend.
		verifyOperationOrder(t, sorted, []struct {
			operation       registry.Operation
			mustBeforeNames []string
			mustAfterNames  []string
		}{
			{
				operation:       updateOp("test", "database").Build(),
				mustBeforeNames: []string{"new-api", "new-frontend"},
			},
			{
				operation:       createOp("test", "new-api").Build(),
				mustBeforeNames: []string{"new-frontend"},
				mustAfterNames:  []string{"database"},
			},
		})
	})

	t.Run("Group Dependencies in Wrong Order", func(t *testing.T) {
		fromState := registry.State{}

		// Wrong order: dependent first, group members last
		cs := NewChangeSet().
			AddOp(createOp("test", "frontend").DependsOn("group:backend")). // Dependent first
			AddOp(createOp("test", "database").Groups("backend")).          // Group member 1
			AddOp(createOp("test", "api").Groups("backend")).               // Group member 2
			AddOp(createOp("test", "cache").Groups("backend")).             // Group member 3
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should fix order: all backend group members before frontend
		verifyOperationOrder(t, sorted, []struct {
			operation       registry.Operation
			mustBeforeNames []string
			mustAfterNames  []string
		}{
			{
				operation:       createOp("test", "database").Build(),
				mustBeforeNames: []string{"frontend"},
			},
			{
				operation:       createOp("test", "api").Build(),
				mustBeforeNames: []string{"frontend"},
			},
			{
				operation:       createOp("test", "cache").Build(),
				mustBeforeNames: []string{"frontend"},
			},
		})
	})
}
