package loader

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

// deepEqualPayloads compares two payloads, assuming the Data field is always a string.
func deepEqualPayloads(p1, p2 payload.Payload) bool {
	if p1.Format() != p2.Format() {
		return false
	}

	// Only compare as strings
	s1, ok1 := p1.Data().(string)
	s2, ok2 := p2.Data().(string)
	if !ok1 || !ok2 {
		return false // Or panic, if non-string is considered an error
	}
	return s1 == s2
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
				{
					ID:   "service.url",
					Kind: "listener",
					Meta: map[string]any{"env": "dev"},
					Data: payload.New("localhost:8080"),
				},
			},
			want: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "service.url",
						Kind: "listener",
						Meta: map[string]any{"env": "dev"},
						Data: payload.New("localhost:8080"),
					},
				},
			},
		},
		{
			name: "mixed data types",
			entries: []registry.Entry{
				{
					ID:   "listener.number",
					Kind: "listener",
					Data: payload.New("123"),
				},
				{
					ID:   "listener.bool",
					Kind: "listener",
					Data: payload.New("true"),
				},
				{
					ID:   "listener.string",
					Kind: "listener",
					Data: payload.New("hello"),
				},
				{
					ID:   "listener.map",
					Kind: "listener",
					Data: payload.New(`{"a": 1, "b": 2}`),
				},
				{
					ID:   "listener.slice",
					Kind: "listener",
					Data: payload.New("[1, 2, 3]"),
				},
			},
			want: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "listener.bool",
						Kind: "listener",
						Data: payload.New("true"),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "listener.map",
						Kind: "listener",
						Data: payload.New(`{"a": 1, "b": 2}`),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "listener.number",
						Kind: "listener",
						Data: payload.New("123"),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "listener.slice",
						Kind: "listener",
						Data: payload.New("[1, 2, 3]"),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "listener.string",
						Kind: "listener",
						Data: payload.New("hello"),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreateChangeSetFromEntries(tt.entries)
			if len(got) != len(tt.want) {
				t.Errorf("CreateChangeSetFromEntries() = %v, want %v", got, tt.want)
				return
			}

			for i := range got {
				if got[i].Kind != tt.want[i].Kind {
					t.Errorf("CreateChangeSetFromEntries() Kind = %v, want %v", got[i].Kind, tt.want[i].Kind)
					return
				}

				if got[i].Entry.ID != tt.want[i].Entry.ID {
					t.Errorf("CreateChangeSetFromEntries() Name = %v, want %v", got[i].Entry.ID, tt.want[i].Entry.ID)
					return
				}

				if !reflect.DeepEqual(got[i].Entry.Meta, tt.want[i].Entry.Meta) {
					t.Errorf("CreateChangeSetFromEntries() Meta = %v, want %v", got[i].Entry.Meta, tt.want[i].Entry.Meta)
					return
				}

				if !deepEqualPayloads(got[i].Entry.Data, tt.want[i].Entry.Data) {
					t.Errorf("CreateChangeSetFromEntries() Payload = %v, want %v", got[i].Entry.Data, tt.want[i].Entry.Data)
					return
				}
			}
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
			name: "simple dependency chain",
			entries: []registry.Entry{
				{
					ID:   "service.database.url",
					Kind: "listener",
					Meta: map[string]any{
						registry.DependsOnTag: []string{"service.database"},
					},
					Data: payload.New("db://prod"),
				},
				{
					ID:   "service.database",
					Kind: "component",
					Meta: map[string]any{
						registry.DependsOnTag: []string{"service"},
					},
					Data: payload.New(""),
				},
				{
					ID:   "service",
					Kind: "service",
					Meta: map[string]any{},
					Data: payload.New(""),
				},
			},
			want: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "service",
						Kind: "service",
						Meta: map[string]any{},
						Data: payload.New(""),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "service.database",
						Kind: "component",
						Meta: map[string]any{
							registry.DependsOnTag: []string{"service"},
						},
						Data: payload.New(""),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "service.database.url",
						Kind: "listener",
						Meta: map[string]any{
							registry.DependsOnTag: []string{"service.database"},
						},
						Data: payload.New("db://prod"),
					},
				},
			},
		},
		{
			name: "multiple dependencies at same level",
			entries: []registry.Entry{
				{
					ID:   "service.cache",
					Kind: "component",
					Meta: map[string]any{
						registry.DependsOnTag: []string{"service"},
					},
					Data: payload.New(""),
				},
				{
					ID:   "service.database",
					Kind: "component",
					Meta: map[string]any{
						registry.DependsOnTag: []string{"service"},
					},
					Data: payload.New(""),
				},
				{
					ID:   "service",
					Kind: "service",
					Meta: map[string]any{},
					Data: payload.New(""),
				},
			},
			want: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "service",
						Kind: "service",
						Meta: map[string]any{},
						Data: payload.New(""),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "service.cache",
						Kind: "component",
						Meta: map[string]any{
							registry.DependsOnTag: []string{"service"},
						},
						Data: payload.New(""),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "service.database",
						Kind: "component",
						Meta: map[string]any{
							registry.DependsOnTag: []string{"service"},
						},
						Data: payload.New(""),
					},
				},
			},
		},
		{
			name: "complex dependencies with multiple levels",
			entries: []registry.Entry{
				{
					ID:   "service.metrics",
					Kind: "component",
					Meta: map[string]any{
						registry.DependsOnTag: []string{"service", "service.database"},
					},
					Data: payload.New(""),
				},
				{
					ID:   "service.database.url",
					Kind: "listener",
					Meta: map[string]any{
						registry.DependsOnTag: []string{"service.database"},
					},
					Data: payload.New("db://prod"),
				},
				{
					ID:   "service.database",
					Kind: "component",
					Meta: map[string]any{
						registry.DependsOnTag: []string{"service"},
					},
					Data: payload.New(""),
				},
				{
					ID:   "service",
					Kind: "service",
					Meta: map[string]any{},
					Data: payload.New(""),
				},
			},
			want: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "service",
						Kind: "service",
						Meta: map[string]any{},
						Data: payload.New(""),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "service.database",
						Kind: "component",
						Meta: map[string]any{
							registry.DependsOnTag: []string{"service"},
						},
						Data: payload.New(""),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "service.database.url",
						Kind: "listener",
						Meta: map[string]any{
							registry.DependsOnTag: []string{"service.database"},
						},
						Data: payload.New("db://prod"),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "service.metrics",
						Kind: "component",
						Meta: map[string]any{
							registry.DependsOnTag: []string{"service", "service.database"},
						},
						Data: payload.New(""),
					},
				},
			},
		},
		{
			name: "cyclic dependencies fallback to lexicographical sort",
			entries: []registry.Entry{
				{
					ID:   "service.b",
					Kind: "component",
					Meta: map[string]any{
						registry.DependsOnTag: []string{"service.c"},
					},
					Data: payload.New(""),
				},
				{
					ID:   "service.c",
					Kind: "component",
					Meta: map[string]any{
						registry.DependsOnTag: []string{"service.b"},
					},
					Data: payload.New(""),
				},
				{
					ID:   "service.a",
					Kind: "component",
					Meta: map[string]any{},
					Data: payload.New(""),
				},
			},
			want: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "service.a",
						Kind: "component",
						Meta: map[string]any{},
						Data: payload.New(""),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "service.b",
						Kind: "component",
						Meta: map[string]any{
							registry.DependsOnTag: []string{"service.c"},
						},
						Data: payload.New(""),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						ID:   "service.c",
						Kind: "component",
						Meta: map[string]any{
							registry.DependsOnTag: []string{"service.b"},
						},
						Data: payload.New(""),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreateChangeSetFromEntries(tt.entries)
			if len(got) != len(tt.want) {
				t.Errorf("CreateChangeSetFromEntries() = %v entries, want %v entries", len(got), len(tt.want))
				return
			}

			for i := range got {
				if got[i].Kind != tt.want[i].Kind {
					t.Errorf("CreateChangeSetFromEntries()[%d].Kind = %v, want %v", i, got[i].Kind, tt.want[i].Kind)
				}
				if got[i].Entry.ID != tt.want[i].Entry.ID {
					t.Errorf("CreateChangeSetFromEntries()[%d].Entry.Name = %v, want %v", i, got[i].Entry.ID, tt.want[i].Entry.ID)
				}
				if !reflect.DeepEqual(got[i].Entry.Meta, tt.want[i].Entry.Meta) {
					t.Errorf("CreateChangeSetFromEntries()[%d].Entry.Meta = %v, want %v", i, got[i].Entry.Meta, tt.want[i].Entry.Meta)
				}
				if !deepEqualPayloads(got[i].Entry.Data, tt.want[i].Entry.Data) {
					t.Errorf("CreateChangeSetFromEntries()[%d].Entry.Data = %v, want %v", i, got[i].Entry.Data, tt.want[i].Entry.Data)
				}
			}
		})
	}
}

func TestCreateChangeSetFromEntries_GroupDependencies(t *testing.T) {
	tests := []struct {
		name    string
		entries []registry.Entry
		// Instead of strict ordering, we define a function to validate dependency constraints.
		validate func(changeSet registry.ChangeSet) error
	}{
		{
			name: "simple group dependency",
			entries: []registry.Entry{
				{
					ID:   "entry.a",
					Kind: "component",
					Meta: map[string]any{
						registry.GroupsTag: []string{"group1"},
					},
					Data: payload.New("A"),
				},
				{
					ID:   "entry.b",
					Kind: "component",
					Meta: map[string]any{
						registry.DependsOnGroupsTag: []string{"group1"},
					},
					Data: payload.New("B"),
				},
				{
					ID:   "entry.c",
					Kind: "component",
					Meta: map[string]any{},
					Data: payload.New("C"),
				},
			},
			validate: func(cs registry.ChangeSet) error {
				// For "entry.b" (which depends on group1) ensure that all entries that are members of group1 appear before it.
				var posA, posB int = -1, -1
				for i, op := range cs {
					switch op.Entry.ID {
					case "entry.a":
						posA = i
					case "entry.b":
						posB = i
					}
				}
				if posA == -1 || posB == -1 {
					return fmt.Errorf("expected entries missing")
				}
				if posA > posB {
					return fmt.Errorf("entry 'entry.a' (group member) should appear before 'entry.b' (depends on group)")
				}
				return nil
			},
		},
		{
			name: "member on multiple groups with multi group dependency",
			entries: []registry.Entry{
				{
					ID:   "entry.x",
					Kind: "component",
					Meta: map[string]any{
						registry.GroupsTag: []string{"group1", "group2"},
					},
					Data: payload.New("X"),
				},
				{
					ID:   "entry.y",
					Kind: "component",
					Meta: map[string]any{
						registry.DependsOnGroupsTag: []string{"group1"},
					},
					Data: payload.New("Y"),
				},
				{
					ID:   "entry.z",
					Kind: "component",
					Meta: map[string]any{
						registry.DependsOnGroupsTag: []string{"group2"},
					},
					Data: payload.New("Z"),
				},
				{
					ID:   "entry.w",
					Kind: "component",
					Meta: map[string]any{},
					Data: payload.New("W"),
				},
			},
			validate: func(cs registry.ChangeSet) error {
				// Verify that entry.x appears before both entry.y and entry.z.
				var posX, posY, posZ int = -1, -1, -1
				for i, op := range cs {
					switch op.Entry.ID {
					case "entry.x":
						posX = i
					case "entry.y":
						posY = i
					case "entry.z":
						posZ = i
					}
				}
				if posX == -1 || posY == -1 || posZ == -1 {
					return fmt.Errorf("missing expected entries in changeset")
				}
				if posX > posY {
					return fmt.Errorf("entry 'entry.x' (group member) should appear before 'entry.y' (depends on group)")
				}
				if posX > posZ {
					return fmt.Errorf("entry 'entry.x' (group member) should appear before 'entry.z' (depends on group)")
				}
				return nil
			},
		},
		{
			name: "complex scenario: multiple members and dependencies",
			entries: []registry.Entry{
				{
					ID:   "entry.1",
					Kind: "component",
					Meta: map[string]any{
						registry.GroupsTag: []string{"groupA"},
					},
					Data: payload.New("1"),
				},
				{
					ID:   "entry.2",
					Kind: "component",
					Meta: map[string]any{
						registry.GroupsTag: []string{"groupB"},
					},
					Data: payload.New("2"),
				},
				{
					ID:   "entry.3",
					Kind: "component",
					Meta: map[string]any{
						registry.GroupsTag: []string{"groupA", "groupB"},
					},
					Data: payload.New("3"),
				},
				{
					ID:   "entry.4",
					Kind: "component",
					Meta: map[string]any{
						registry.DependsOnGroupsTag: []string{"groupA", "groupB"},
					},
					Data: payload.New("4"),
				},
				{
					ID:   "entry.5",
					Kind: "component",
					Meta: map[string]any{},
					Data: payload.New("5"),
				},
			},
			validate: func(cs registry.ChangeSet) error {
				// entry.4 should appear after every entry that belongs to groupA or groupB (i.e. entry.1, entry.2, and entry.3).
				pos := make(map[string]int)
				for i, op := range cs {
					pos[string(op.Entry.ID)] = i
				}
				for _, member := range []string{"entry.1", "entry.2", "entry.3"} {
					if pos[member] >= pos["entry.4"] {
						return fmt.Errorf("entry '%s' (group member) should appear before 'entry.4' (depends on groups)", member)
					}
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := CreateChangeSetFromEntries(tt.entries)
			if err := tt.validate(cs); err != nil {
				t.Errorf("Validation failed: %v", err)
			}

			// Ensure the changeset includes every entry.
			if len(cs) != len(tt.entries) {
				t.Errorf("expected changeset length %d, got %d", len(tt.entries), len(cs))
			}
		})
	}
}
