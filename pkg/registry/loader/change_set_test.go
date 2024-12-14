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
					Path: "service.url",
					Kind: "config",
					Meta: map[string]any{"env": "dev"},
					Data: payload.New("localhost:8080"),
				},
			},
			want: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						Path: "service.url",
						Kind: "config",
						Meta: map[string]any{"env": "dev"},
						Data: payload.New("localhost:8080"),
					},
				},
			},
		},
		{
			name: "multiple entries with sorting",
			entries: []registry.Entry{
				{
					Path: "service.database.url",
					Kind: "config",
					Meta: map[string]any{"env": "prod"},
					Data: payload.New("db://prod"),
				},
				{
					Path: "service.cache.size",
					Kind: "config",
					Meta: map[string]any{"unit": "MB"},
					Data: payload.New("1024"),
				},
				{
					Path: "gateway.port",
					Kind: "endpoint",
					Meta: map[string]any{"protocol": "http"},
					Data: payload.New("8080"),
				},
				{
					Path: "service",
					Kind: "service",
					Meta: map[string]any{"version": "1.0"},
					Data: payload.New(""),
				},
				{
					Path: "service.database",
					Kind: "component",
					Meta: map[string]any{"type": "sql"},
					Data: payload.New(""),
				},
				{
					Path: "another.service.key",
					Kind: "config",
					Meta: map[string]any{"unit": "MB"},
					Data: payload.New("2024"),
				},
			},
			want: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						Path: "service",
						Kind: "service",
						Meta: map[string]any{"version": "1.0"},
						Data: payload.New(""),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						Path: "gateway.port",
						Kind: "endpoint",
						Meta: map[string]any{"protocol": "http"},
						Data: payload.New("8080"),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						Path: "service.database",
						Kind: "component",
						Meta: map[string]any{"type": "sql"},
						Data: payload.New(""),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						Path: "another.service.key",
						Kind: "config",
						Meta: map[string]any{"unit": "MB"},
						Data: payload.New("2024"),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						Path: "service.cache.size",
						Kind: "config",
						Meta: map[string]any{"unit": "MB"},
						Data: payload.New("1024"),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						Path: "service.database.url",
						Kind: "config",
						Meta: map[string]any{"env": "prod"},
						Data: payload.New("db://prod"),
					},
				},
			},
		},
		{
			name: "multiple entries already sorted",
			entries: []registry.Entry{
				{
					Path: "app",
					Kind: "service",
					Meta: map[string]any{"version": "1.0"},
					Data: payload.New(""),
				},
				{
					Path: "app.cache",
					Kind: "config",
					Meta: map[string]any{"type": "redis"},
					Data: payload.New(""),
				},
				{
					Path: "app.config",
					Kind: "config",
					Meta: map[string]any{"env": "dev"},
					Data: payload.New("localhost:8080"),
				},
				{
					Path: "app.config.logging",
					Kind: "config",
					Meta: map[string]any{"level": "info"},
					Data: payload.New("default"),
				},
			},
			want: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						Path: "app",
						Kind: "service",
						Meta: map[string]any{"version": "1.0"},
						Data: payload.New(""),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						Path: "app.cache",
						Kind: "config",
						Meta: map[string]any{"type": "redis"},
						Data: payload.New(""),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						Path: "app.config",
						Kind: "config",
						Meta: map[string]any{"env": "dev"},
						Data: payload.New("localhost:8080"),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						Path: "app.config.logging",
						Kind: "config",
						Meta: map[string]any{"level": "info"},
						Data: payload.New("default"),
					},
				},
			},
		},
		{
			name: "mixed data types",
			entries: []registry.Entry{
				{
					Path: "config.number",
					Kind: "config",
					Data: payload.New("123"),
				},
				{
					Path: "config.bool",
					Kind: "config",
					Data: payload.New("true"),
				},
				{
					Path: "config.string",
					Kind: "config",
					Data: payload.New("hello"),
				},
				{
					Path: "config.map",
					Kind: "config",
					Data: payload.New(`{"a": 1, "b": 2}`),
				},
				{
					Path: "config.slice",
					Kind: "config",
					Data: payload.New("[1, 2, 3]"),
				},
			},
			want: registry.ChangeSet{
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						Path: "config.bool",
						Kind: "config",
						Data: payload.New("true"),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						Path: "config.map",
						Kind: "config",
						Data: payload.New(`{"a": 1, "b": 2}`),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						Path: "config.number",
						Kind: "config",
						Data: payload.New("123"),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						Path: "config.slice",
						Kind: "config",
						Data: payload.New("[1, 2, 3]"),
					},
				},
				{
					Kind: registry.Create,
					Entry: registry.Entry{
						Path: "config.string",
						Kind: "config",
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

				if got[i].Entry.Path != tt.want[i].Entry.Path {
					t.Errorf("CreateChangeSetFromEntries() Path = %v, want %v", got[i].Entry.Path, tt.want[i].Entry.Path)
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
