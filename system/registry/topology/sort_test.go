package topology

import (
	"fmt"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

// Helper function to create an ID with just a name (no namespace)
func id(name string) registry.ID {
	return registry.ID{Name: name}
}

// Helper function to create an ID with both namespace and name
func nsID(namespace, name string) registry.ID {
	return registry.ID{NS: namespace, Name: name}
}

// deepEqualPayloads compares two payloads, assuming the Data field is always a string.
func deepEqualPayloads(p1, p2 payload.Payload) bool {
	if p1.Format() != p2.Format() {
		return false
	}

	// Only compare as strings
	s1, ok1 := p1.Data().(string)
	s2, ok2 := p2.Data().(string)
	if !ok1 || !ok2 {
		return false
	}
	return s1 == s2
}

// Helper function to create a basic entry
func makeEntry(id registry.ID, kind string, data string) registry.Entry {
	return registry.Entry{
		ID:   id,
		Kind: kind,
		Meta: map[string]any{},
		Data: payload.New(data),
	}
}

// Helper function to create an entry with metadata
func makeEntryWithMeta(id registry.ID, kind string, data string, meta map[string]any) registry.Entry {
	return registry.Entry{
		ID:   id,
		Kind: kind,
		Meta: meta,
		Data: payload.New(data),
	}
}

// Helper function to compare change sets and report detailed differences
func compareChangeSets(t *testing.T, got, want registry.ChangeSet) bool {
	t.Helper()

	if len(got) != len(want) {
		t.Errorf("ChangeSet length mismatch: got %d, want %d", len(got), len(want))
		return false
	}

	for i := range got {
		if got[i].Kind != want[i].Kind {
			t.Errorf("Operation[%d].Kind mismatch: got %v, want %v", i, got[i].Kind, want[i].Kind)
			return false
		}

		if got[i].Entry.ID != want[i].Entry.ID {
			t.Errorf("Operation[%d].Entry.ID mismatch: got %v, want %v", i, got[i].Entry.ID, want[i].Entry.ID)
			return false
		}

		if !deepEqualPayloads(got[i].Entry.Data, want[i].Entry.Data) {
			t.Errorf("Operation[%d].Entry.Data mismatch: got %v, want %v", i, got[i].Entry.Data, want[i].Entry.Data)
			return false
		}
	}

	return true
}

func TestCreateChangeSetFromEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries []registry.Entry
		want    registry.ChangeSet
	}{
		{
			name:    "empty input",
			entries: []registry.Entry{},
			want:    nil,
		},
		{
			name: "single entry",
			entries: []registry.Entry{
				makeEntryWithMeta(
					id("service.url"),
					"listener",
					"localhost:8080",
					map[string]any{"env": "dev"},
				),
			},
			want: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: makeEntryWithMeta(
						id("service.url"),
						"listener",
						"localhost:8080",
						map[string]any{"env": "dev"},
					),
				},
			},
		},
		{
			name: "mixed data types",
			entries: []registry.Entry{
				makeEntry(id("listener.number"), "listener", "123"),
				makeEntry(id("listener.bool"), "listener", "true"),
				makeEntry(id("listener.string"), "listener", "hello"),
				makeEntry(id("listener.map"), "listener", `{"a": 1, "b": 2}`),
				makeEntry(id("listener.slice"), "listener", "[1, 2, 3]"),
			},
			want: registry.ChangeSet{
				{
					Kind:  registry.Create,
					Entry: makeEntry(id("listener.bool"), "listener", "true"),
				},
				{
					Kind:  registry.Create,
					Entry: makeEntry(id("listener.map"), "listener", `{"a": 1, "b": 2}`),
				},
				{
					Kind:  registry.Create,
					Entry: makeEntry(id("listener.number"), "listener", "123"),
				},
				{
					Kind:  registry.Create,
					Entry: makeEntry(id("listener.slice"), "listener", "[1, 2, 3]"),
				},
				{
					Kind:  registry.Create,
					Entry: makeEntry(id("listener.string"), "listener", "hello"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreateChangeSetFromEntries(tt.entries)
			compareChangeSets(t, got, tt.want)
		})
	}
}

func TestCreateChangeSetFromEntries_Dependencies(t *testing.T) {
	tests := []struct {
		name    string
		entries []registry.Entry
		want    registry.ChangeSet
	}{
		{
			name: "direct dependency with namespace inheritance",
			entries: []registry.Entry{
				makeEntryWithMeta(
					nsID("ns1", "service.api"),
					"service",
					"api",
					map[string]any{
						registry.TagDependsOn: []string{"service.db"}, // should inherit ns1
					},
				),
				makeEntryWithMeta(
					nsID("ns1", "service.db"),
					"service",
					"db",
					map[string]any{},
				),
			},
			want: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: makeEntryWithMeta(
						nsID("ns1", "service.db"),
						"service",
						"db",
						map[string]any{},
					),
				},
				{
					Kind: registry.Create,
					Entry: makeEntryWithMeta(
						nsID("ns1", "service.api"),
						"service",
						"api",
						map[string]any{
							registry.TagDependsOn: []string{"service.db"},
						},
					),
				},
			},
		},
		{
			name: "mixed dependency types",
			entries: []registry.Entry{
				makeEntryWithMeta(
					nsID("backend", "service.api"),
					"service",
					"api",
					map[string]any{
						registry.TagGroups: []string{"public-apis"},
					},
				),
				makeEntryWithMeta(
					nsID("frontend", "webapp"),
					"webapp",
					"webapp",
					map[string]any{
						registry.TagDependsOn: []string{
							"service.cache",       // implicit ns:frontend
							"backend:service.api", // explicit ns reference
							"group:public-apis",   // group reference
							"ns:backend",          // namespace reference
						},
					},
				),
				makeEntryWithMeta(
					nsID("frontend", "service.cache"),
					"service",
					"cache",
					map[string]any{},
				),
			},
			want: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: makeEntryWithMeta(
						nsID("backend", "service.api"),
						"service",
						"api",
						map[string]any{
							registry.TagGroups: []string{"public-apis"},
						},
					),
				},
				{
					Kind: registry.Create,
					Entry: makeEntryWithMeta(
						nsID("frontend", "service.cache"),
						"service",
						"cache",
						map[string]any{},
					),
				},
				{
					Kind: registry.Create,
					Entry: makeEntryWithMeta(
						nsID("frontend", "webapp"),
						"webapp",
						"webapp",
						map[string]any{
							registry.TagDependsOn: []string{
								"service.cache",
								"backend:service.api",
								"group:public-apis",
								"ns:backend",
							},
						},
					),
				},
			},
		},
		{
			name: "cyclic dependencies with different reference types",
			entries: []registry.Entry{
				makeEntryWithMeta(
					nsID("ns1", "service.a"),
					"service",
					"a",
					map[string]any{
						registry.TagDependsOn: []string{"group:group-b"},
						registry.TagGroups:    []string{"group-a"},
					},
				),
				makeEntryWithMeta(
					nsID("ns2", "service.b"),
					"service",
					"b",
					map[string]any{
						registry.TagDependsOn: []string{"group:group-a"},
						registry.TagGroups:    []string{"group-b"},
					},
				),
			},
			want: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: makeEntryWithMeta(
						nsID("ns1", "service.a"),
						"service",
						"a",
						map[string]any{
							registry.TagDependsOn: []string{"group:group-b"},
							registry.TagGroups:    []string{"group-a"},
						},
					),
				},
				{
					Kind: registry.Create,
					Entry: makeEntryWithMeta(
						nsID("ns2", "service.b"),
						"service",
						"b",
						map[string]any{
							registry.TagDependsOn: []string{"group:group-a"},
							registry.TagGroups:    []string{"group-b"},
						},
					),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreateChangeSetFromEntries(tt.entries)
			compareChangeSets(t, got, tt.want)
		})
	}
}

func TestCreateChangeSetFromEntries_GroupDependencies(t *testing.T) {
	tests := []struct {
		name     string
		entries  []registry.Entry
		validate func(registry.ChangeSet) error
	}{
		{
			name: "simple group dependency",
			entries: []registry.Entry{
				makeEntryWithMeta(
					id("entry.a"),
					"component",
					"A",
					map[string]any{
						registry.TagGroups: []string{"group1"},
					},
				),
				makeEntryWithMeta(
					id("entry.b"),
					"component",
					"B",
					map[string]any{
						registry.TagDependsOn: []string{"group:group1"},
					},
				),
				makeEntry(id("entry.c"), "component", "C"),
			},
			validate: func(cs registry.ChangeSet) error {
				posMap := make(map[string]int)
				for i, op := range cs {
					posMap[op.Entry.ID.Name] = i
				}

				if posMap["entry.a"] > posMap["entry.b"] {
					return fmt.Errorf("entry 'entry.a' (group member) should appear before 'entry.b' (depends on group)")
				}
				return nil
			},
		},
		{
			name: "member in multiple groups with multi group dependency",
			entries: []registry.Entry{
				makeEntryWithMeta(
					id("entry.x"),
					"component",
					"X",
					map[string]any{
						registry.TagGroups: []string{"group1", "group2"},
					},
				),
				makeEntryWithMeta(
					id("entry.y"),
					"component",
					"Y",
					map[string]any{
						registry.TagDependsOn: []string{"group:group1"},
					},
				),
				makeEntryWithMeta(
					id("entry.z"),
					"component",
					"Z",
					map[string]any{
						registry.TagDependsOn: []string{"group:group2"},
					},
				),
			},
			validate: func(cs registry.ChangeSet) error {
				posMap := make(map[string]int)
				for i, op := range cs {
					posMap[op.Entry.ID.Name] = i
				}

				if posMap["entry.x"] > posMap["entry.y"] {
					return fmt.Errorf("entry 'entry.x' (group1 member) should appear before 'entry.y'")
				}
				if posMap["entry.x"] > posMap["entry.z"] {
					return fmt.Errorf("entry 'entry.x' (group2 member) should appear before 'entry.z'")
				}
				return nil
			},
		},
		{
			name: "combined direct and group dependencies",
			entries: []registry.Entry{
				makeEntryWithMeta(
					id("base"),
					"component",
					"base",
					map[string]any{
						registry.TagGroups: []string{"base-group"},
					},
				),
				makeEntryWithMeta(
					id("mid"),
					"component",
					"mid",
					map[string]any{
						registry.TagDependsOn: []string{"group:base-group"},
						registry.TagGroups:    []string{"mid-group"},
					},
				),
				makeEntryWithMeta(
					id("top"),
					"component",
					"top",
					map[string]any{
						registry.TagDependsOn: []string{
							"group:mid-group",
							"mid",
						},
					},
				),
			},
			validate: func(cs registry.ChangeSet) error {
				posMap := make(map[string]int)
				for i, op := range cs {
					posMap[op.Entry.ID.Name] = i
				}

				if posMap["base"] > posMap["mid"] {
					return fmt.Errorf("'base' should appear before 'mid'")
				}
				if posMap["mid"] > posMap["top"] {
					return fmt.Errorf("'mid' should appear before 'top'")
				}
				return nil
			},
		},
		{
			name: "complex scenario with multiple groups and dependencies",
			entries: []registry.Entry{
				makeEntryWithMeta(
					id("entry.1"),
					"component",
					"1",
					map[string]any{
						registry.TagGroups: []string{"groupA"},
					},
				),
				makeEntryWithMeta(
					id("entry.2"),
					"component",
					"2",
					map[string]any{
						registry.TagGroups: []string{"groupB"},
					},
				),
				makeEntryWithMeta(
					id("entry.3"),
					"component",
					"3",
					map[string]any{
						registry.TagGroups: []string{"groupA", "groupB"},
					},
				),
				makeEntryWithMeta(
					id("entry.4"),
					"component",
					"4",
					map[string]any{
						registry.TagDependsOn: []string{"group:groupA", "group:groupB"},
					},
				),
			},
			validate: func(cs registry.ChangeSet) error {
				posMap := make(map[string]int)
				for i, op := range cs {
					posMap[op.Entry.ID.Name] = i
				}

				groupMembers := []string{"entry.1", "entry.2", "entry.3"}
				for _, member := range groupMembers {
					if posMap[member] > posMap["entry.4"] {
						return fmt.Errorf("entry '%s' (group member) should appear before 'entry.4'", member)
					}
				}
				return nil
			},
		},
		{
			name: "group dependency with namespace",
			entries: []registry.Entry{
				makeEntryWithMeta(
					nsID("ns1", "component.a"),
					"component",
					"A",
					map[string]any{
						registry.TagGroups: []string{"group1"},
					},
				),
				makeEntryWithMeta(
					nsID("ns2", "component.b"),
					"component",
					"B",
					map[string]any{
						registry.TagDependsOn: []string{"group:group1"},
					},
				),
			},
			validate: func(cs registry.ChangeSet) error {
				var posA, posB int = -1, -1
				for i, op := range cs {
					if op.Entry.ID.NS == "ns1" && op.Entry.ID.Name == "component.a" {
						posA = i
					}
					if op.Entry.ID.NS == "ns2" && op.Entry.ID.Name == "component.b" {
						posB = i
					}
				}
				if posA == -1 || posB == -1 {
					return fmt.Errorf("missing expected entries")
				}
				if posA > posB {
					return fmt.Errorf("entry from ns1 (group member) should appear before entry from ns2")
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := CreateChangeSetFromEntries(tt.entries)

			// Verify changeset length matches input
			if len(cs) != len(tt.entries) {
				t.Errorf("expected changeset length %d, got %d", len(tt.entries), len(cs))
				return
			}

			// Run the validation function
			if err := tt.validate(cs); err != nil {
				t.Errorf("validation failed: %v", err)
			}

			// Verify all entries are present
			expectedIDs := make(map[registry.ID]bool)
			for _, entry := range tt.entries {
				expectedIDs[entry.ID] = false
			}
			for _, op := range cs {
				if _, exists := expectedIDs[op.Entry.ID]; !exists {
					t.Errorf("unexpected entry ID in result: %v", op.Entry.ID)
				}
				expectedIDs[op.Entry.ID] = true
			}
			for id, found := range expectedIDs {
				if !found {
					t.Errorf("missing entry ID in result: %v", id)
				}
			}
		})
	}
}

func TestCreateChangeSetFromEntries_ImplicitNamespaceGroup(t *testing.T) {
	tests := []struct {
		name     string
		entries  []registry.Entry
		validate func(registry.ChangeSet) error
	}{
		{
			name: "implicit namespace group dependency via depends_on_groups",
			entries: []registry.Entry{
				makeEntryWithMeta(
					nsID("backend", "service.api"),
					"service",
					"api",
					map[string]any{},
				),
				makeEntryWithMeta(
					nsID("frontend", "app"),
					"webapp",
					"app",
					map[string]any{
						registry.TagDependsOn: []string{"ns:backend"},
					},
				),
			},
			validate: func(cs registry.ChangeSet) error {
				var apiPos, appPos int = -1, -1
				for i, op := range cs {
					if op.Entry.ID.NS == "backend" && op.Entry.ID.Name == "service.api" {
						apiPos = i
					}
					if op.Entry.ID.NS == "frontend" && op.Entry.ID.Name == "app" {
						appPos = i
					}
				}
				if apiPos == -1 || appPos == -1 {
					return fmt.Errorf("missing expected entries")
				}
				if apiPos > appPos {
					return fmt.Errorf("backend service should appear before frontend app")
				}
				return nil
			},
		},
		{
			name: "multiple entries in namespace with external dependency",
			entries: []registry.Entry{
				makeEntryWithMeta(
					nsID("shared", "config"),
					"config",
					"shared-config",
					map[string]any{},
				),
				makeEntryWithMeta(
					nsID("backend", "service.auth"),
					"service",
					"auth",
					map[string]any{},
				),
				makeEntryWithMeta(
					nsID("backend", "service.api"),
					"service",
					"api",
					map[string]any{},
				),
				makeEntryWithMeta(
					nsID("frontend", "app"),
					"webapp",
					"app",
					map[string]any{
						registry.TagDependsOn: []string{"ns:backend", "ns:shared"},
					},
				),
			},
			validate: func(cs registry.ChangeSet) error {
				posMap := make(map[string]int)
				for i, op := range cs {
					key := op.Entry.ID.NS + ":" + op.Entry.ID.Name
					posMap[key] = i
				}

				// Check that shared config comes before frontend app
				if posMap["shared:config"] > posMap["frontend:app"] {
					return fmt.Errorf("shared config should appear before frontend app")
				}

				// Check that both backend services come before frontend app
				if posMap["backend:service.auth"] > posMap["frontend:app"] {
					return fmt.Errorf("backend auth should appear before frontend app")
				}
				if posMap["backend:service.api"] > posMap["frontend:app"] {
					return fmt.Errorf("backend api should appear before frontend app")
				}

				return nil
			},
		},
		{
			name: "mixed explicit and implicit namespace group dependencies",
			entries: []registry.Entry{
				makeEntryWithMeta(
					nsID("backend", "service.db"),
					"service",
					"db",
					map[string]any{
						registry.TagGroups: []string{"data-services"},
					},
				),
				makeEntryWithMeta(
					nsID("backend", "service.cache"),
					"service",
					"cache",
					map[string]any{
						registry.TagGroups: []string{"data-services"},
					},
				),
				makeEntryWithMeta(
					nsID("frontend", "app"),
					"webapp",
					"app",
					map[string]any{
						registry.TagDependsOn: []string{"ns:backend", "group:data-services"},
					},
				),
			},
			validate: func(cs registry.ChangeSet) error {
				frontendPos := -1
				backendServices := make(map[string]int)

				for i, op := range cs {
					if op.Entry.ID.NS == "frontend" {
						frontendPos = i
					}
					if op.Entry.ID.NS == "backend" {
						backendServices[op.Entry.ID.Name] = i
					}
				}

				// Check all backend services come before frontend
				for service, pos := range backendServices {
					if pos > frontendPos {
						return fmt.Errorf("backend service %s should appear before frontend app", service)
					}
				}

				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := CreateChangeSetFromEntries(tt.entries)

			// Verify changeset length matches input
			if len(cs) != len(tt.entries) {
				t.Errorf("expected changeset length %d, got %d", len(tt.entries), len(cs))
				return
			}

			// Run the validation function
			if err := tt.validate(cs); err != nil {
				t.Errorf("validation failed: %v", err)
			}

			// Verify all entries are present
			expectedIDs := make(map[registry.ID]bool)
			for _, entry := range tt.entries {
				expectedIDs[entry.ID] = false
			}
			for _, op := range cs {
				if _, exists := expectedIDs[op.Entry.ID]; !exists {
					t.Errorf("unexpected entry ID in result: %v", op.Entry.ID)
				}
				expectedIDs[op.Entry.ID] = true
			}
			for id, found := range expectedIDs {
				if !found {
					t.Errorf("missing entry ID in result: %v", id)
				}
			}
		})
	}
}
