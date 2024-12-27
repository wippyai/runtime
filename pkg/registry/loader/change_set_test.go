package loader

import (
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
					t.Errorf("CreateChangeSetFromEntries()[%d].Entry.ID = %v, want %v", i, got[i].Entry.ID, tt.want[i].Entry.ID)
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
