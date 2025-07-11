package topology

import (
	"strings"
	"testing"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
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
		kind: registry.Create,
		ns:   ns,
		name: name,
	}
}

func updateOp(ns, name string) *opBuilder {
	return &opBuilder{
		kind: registry.Update,
		ns:   ns,
		name: name,
	}
}

func deleteOp(ns, name string) *opBuilder {
	return &opBuilder{
		kind: registry.Delete,
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
	return registry.ChangeSet(csb.operations)
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
	kindPosMap := make(map[string][]int) // Track positions by kind

	for i, op := range sorted {
		key := op.Entry.ID.Name
		posMap[key] = i
		kindPosMap[op.Kind] = append(kindPosMap[op.Kind], i)
	}

	// Verify operation type ordering: Deletes before Creates/Updates
	if deletePositions, hasDeletes := kindPosMap[registry.Delete]; hasDeletes {
		if createPositions, hasCreates := kindPosMap[registry.Create]; hasCreates {
			lastDelete := deletePositions[len(deletePositions)-1]
			firstCreate := createPositions[0]
			if lastDelete > firstCreate {
				t.Errorf("Operation type ordering violation: all deletes must come before creates")
			}
		}
		if updatePositions, hasUpdates := kindPosMap[registry.Update]; hasUpdates {
			lastDelete := deletePositions[len(deletePositions)-1]
			firstUpdate := updatePositions[0]
			if lastDelete > firstUpdate {
				t.Errorf("Operation type ordering violation: all deletes must come before updates")
			}
		}
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

func TestSortChangeSet_Empty(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop())

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
	builder := NewStateBuilder(zap.NewNop())

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
	builder := NewStateBuilder(zap.NewNop())

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

		// Verify deletes come first, then creates/updates
		if len(sorted) != 3 {
			t.Fatalf("expected 3 operations, got %d", len(sorted))
		}

		// First operation should be delete
		if sorted[0].Kind != registry.Delete {
			t.Errorf("expected first operation to be delete, got %s", sorted[0].Kind)
		}
	})

	t.Run("Mixed With Dependencies", func(t *testing.T) {
		fromState := registry.State{
			testEntry{ns: "test", name: "database", kind: "service", data: "v1"}.toEntry(),
			testEntry{ns: "test", name: "old-service", kind: "service", data: "v1", dependsOn: []string{"database"}}.toEntry(),
		}

		cs := NewChangeSet().
			AddOp(deleteOp("test", "old-service")).                       // Should be first (delete dependent)
			AddOp(updateOp("test", "database").Data("v2")).               // Should be second (update dependency)
			AddOp(createOp("test", "new-service").DependsOn("database")). // Should be last (create dependent)
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
				operation:       deleteOp("test", "old-service").Build(),
				mustBeforeNames: []string{"database", "new-service"},
			},
			{
				operation:       updateOp("test", "database").Build(),
				mustBeforeNames: []string{"new-service"},
			},
		})
	})
}

func TestSortChangeSet_Dependencies(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop())

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

func TestSortChangeSet_ComplexScenarios(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop())

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
			// Delete operations come first
			{
				operation:       deleteOp("app", "old-api").Build(),
				mustBeforeNames: []string{"database", "new-api", "cache", "frontend"},
			},
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

func TestSortChangeSet_CircularDependencies(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop())

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
	builder := NewStateBuilder(zap.NewNop())

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
			// Deletes first
			{
				operation:       deleteOp("infra", "old-cache").Build(),
				mustBeforeNames: []string{"database", "user-service", "auth-service", "api-gateway", "redis-cache", "metrics"},
			},
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
			// Delete old DB first (since services in fromState depend on it)
			{
				operation:       deleteOp("data", "old-db").Build(),
				mustBeforeNames: []string{"new-db", "service-a", "service-b"},
			},
			// New DB before services
			{
				operation:       createOp("data", "new-db").Build(),
				mustBeforeNames: []string{"service-a", "service-b"},
			},
		})
	})
}

func TestSortChangeSet_EdgeCases(t *testing.T) {
	builder := NewStateBuilder(zap.NewNop())

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
	builder := NewStateBuilder(zap.NewNop())

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
			AddOp(deleteOp("test", "old-api")).                           // Delete last instead of first
			Build()

		sorted, err := builder.SortChangeSet(fromState, cs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should fix order to: delete old-api -> update database -> create new-api -> create new-frontend
		verifyOperationOrder(t, sorted, []struct {
			operation       registry.Operation
			mustBeforeNames []string
			mustAfterNames  []string
		}{
			{
				operation:       deleteOp("test", "old-api").Build(),
				mustBeforeNames: []string{"database", "new-api", "new-frontend"},
			},
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
