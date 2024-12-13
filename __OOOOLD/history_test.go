package __OOOOLD

import (
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"testing"
)

func Test_newHistory(t *testing.T) {
	tests := []struct {
		name      string
		wantPanic bool
	}{
		{
			name:      "Valid newHistory",
			wantPanic: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if (r != nil) != tt.wantPanic {
					t.Errorf("newHistory() panic = %v, wantPanic %v", r, tt.wantPanic)
				}
			}()
			got := newHistory()
			if !tt.wantPanic {
				if cap(got.entries) != 0 {
					t.Errorf("newHistory() entries capacity = %v, want %v", cap(got.entries), 0)
				}
				if got.versionSequence != 0 {
					t.Errorf("newHistory() versionSequence = %v, want 0", got.versionSequence)
				}
				if got.actionCounter != 0 {
					t.Errorf("newHistory() actionCounter = %v, want 0", got.actionCounter)
				}
			}
		})
	}
}

func Test_history_addVersion(t *testing.T) {
	type args struct {
		v       registry.Version
		actions []registry.Operation
	}
	tests := []struct {
		name         string
		args         args
		wantErr      bool
		wantHistory  []versionedActions
		wantVersions map[registry.Version][]registry.Entry
	}{
		{
			name: "Add first version",
			args: args{
				v: "reg:v000.000",
				actions: []registry.Operation{
					{Kind: registry.Create, Entry: registry.Entry{Path: "/path/a", Data: payload.NewString("config_a")}},
				},
			},
			wantErr: false,
			wantHistory: []versionedActions{
				{version: "reg:v000.000", actions: []registry.Operation{
					{Kind: registry.Create, Entry: registry.Entry{Path: "/path/a", Data: payload.NewString("config_a")}},
				}},
			},
			wantVersions: map[registry.Version][]registry.Entry{
				"reg:v000.000": {
					{Path: "/path/a", Data: payload.NewString("config_a")},
				},
			},
		},
		{
			name: "Add second version",
			args: args{
				v: "reg:v000.001",
				actions: []registry.Operation{
					{Kind: registry.Create, Entry: registry.Entry{Path: "/path/b", Data: payload.NewString("config_b")}},
				},
			},
			wantErr: false,
			wantHistory: []versionedActions{
				{version: "reg:v000.000", actions: []registry.Operation{
					{Kind: registry.Create, Entry: registry.Entry{Path: "/path/a", Data: payload.NewString("config_a")}},
				}},
				{version: "reg:v000.001", actions: []registry.Operation{
					{Kind: registry.Create, Entry: registry.Entry{Path: "/path/b", Data: payload.NewString("config_b")}},
				}},
			},
			wantVersions: map[registry.Version][]registry.Entry{
				"reg:v000.000": {
					{Path: "/path/a", Data: payload.NewString("config_a")},
				},
				"reg:v000.001": {
					{Path: "/path/a", Data: payload.NewString("config_a")},
					{Path: "/path/b", Data: payload.NewString("config_b")},
				},
			},
		},
		{
			name: "Add updated version",
			args: args{
				v: "reg:v000.002",
				actions: []registry.Operation{
					{Kind: registry.Update, Entry: registry.Entry{Path: "/path/a", Data: payload.NewString("config_a_updated")}},
				},
			},
			wantErr: false,
			wantHistory: []versionedActions{
				{version: "reg:v000.000", actions: []registry.Operation{
					{Kind: registry.Create, Entry: registry.Entry{Path: "/path/a", Data: payload.NewString("config_a")}},
				}},
				{version: "reg:v000.001", actions: []registry.Operation{
					{Kind: registry.Create, Entry: registry.Entry{Path: "/path/b", Data: payload.NewString("config_b")}},
				}},
				{version: "reg:v000.002", actions: []registry.Operation{
					{Kind: registry.Update, Entry: registry.Entry{Path: "/path/a", Data: payload.NewString("config_a_updated")}},
				}},
			},
			wantVersions: map[registry.Version][]registry.Entry{
				"reg:v000.000": {
					{Path: "/path/a", Data: payload.NewString("config_a")},
				},
				"reg:v000.001": {
					{Path: "/path/a", Data: payload.NewString("config_a")},
					{Path: "/path/b", Data: payload.NewString("config_b")},
				},
				"reg:v000.002": {
					{Path: "/path/a", Data: payload.NewString("config_a_updated")},
					{Path: "/path/b", Data: payload.NewString("config_b")},
				},
			},
		},
		{
			name: "Add deleted version",
			args: args{
				v: "reg:v000.003",
				actions: []registry.Operation{
					{Kind: registry.Delete, Entry: registry.Entry{Path: "/path/b"}},
				},
			},
			wantErr: false,
			wantHistory: []versionedActions{
				{version: "reg:v000.000", actions: []registry.Operation{
					{Kind: registry.Create, Entry: registry.Entry{Path: "/path/a", Data: payload.NewString("config_a")}},
				}},
				{version: "reg:v000.001", actions: []registry.Operation{
					{Kind: registry.Create, Entry: registry.Entry{Path: "/path/b", Data: payload.NewString("config_b")}},
				}},
				{version: "reg:v000.002", actions: []registry.Operation{
					{Kind: registry.Update, Entry: registry.Entry{Path: "/path/a", Data: payload.NewString("config_a_updated")}},
				}},
				{version: "reg:v000.003", actions: []registry.Operation{
					{Kind: registry.Delete, Entry: registry.Entry{Path: "/path/b"}},
				}},
			},
			wantVersions: map[registry.Version][]registry.Entry{
				"reg:v000.000": {
					{Path: "/path/a", Data: payload.NewString("config_a")},
				},
				"reg:v000.001": {
					{Path: "/path/a", Data: payload.NewString("config_a")},
					{Path: "/path/b", Data: payload.NewString("config_b")},
				},
				"reg:v000.002": {
					{Path: "/path/a", Data: payload.NewString("config_a_updated")},
					{Path: "/path/b", Data: payload.NewString("config_b")},
				},
				"reg:v000.003": {
					{Path: "/path/a", Data: payload.NewString("config_a_updated")},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHistory()
			//pre-populate storage and versions
			if len(tt.wantHistory) > 1 {
				for i := 0; i < len(tt.wantHistory)-1; i++ {
					h.entries = append(h.entries, tt.wantHistory[i])
					h.versions[tt.wantHistory[i].version] = tt.wantVersions[tt.wantHistory[i].version]
				}
			}

			if err := h.addVersion(tt.args.v, tt.args.actions); (err != nil) != tt.wantErr {
				t.Errorf("storage.addVersion() error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(h.entries) != len(tt.wantHistory) {
				t.Fatalf("storage.addVersion() entries length = %v, want %v", len(h.entries), len(tt.wantHistory))
			}
			for i, entry := range h.entries {
				wantEntry := tt.wantHistory[i]
				if entry.version != wantEntry.version {
					t.Errorf("storage.addVersion() entry version = %v, want %v", entry.version, wantEntry.version)
				}
				if len(entry.actions) != len(wantEntry.actions) {
					t.Fatalf("storage.addVersion() actions length = %v, want %v", len(entry.actions), len(wantEntry.actions))
				}
				for j, action := range entry.actions {
					wantAction := wantEntry.actions[j]
					if action.Kind != wantAction.Kind {
						t.Errorf("storage.addVersion() action Kind = %v, want %v", action.Kind, wantAction.Kind)
					}
					if action.Entry.Path != wantAction.Entry.Path {
						t.Errorf("storage.addVersion() action Entry.Path = %v, want %v", action.Entry.Path, wantAction.Entry.Path)
					}
					if action.Entry.Data.Format() != wantAction.Entry.Data.Format() {
						t.Errorf("storage.addVersion() action Entry.Data format = %v, want %v", action.Entry.Data.Format(), wantAction.Entry.Data.Format())
					}
					if action.Entry.Data.Data() != wantAction.Entry.Data.Data() {
						t.Errorf("storage.addVersion() action Entry.Data data = %v, want %v", action.Entry.Data.Data(), wantAction.Entry.Data.Data())
					}
				}
			}

			if len(h.versions) != len(tt.wantVersions) {
				t.Fatalf("storage.addVersion() versions length = %v, want %v", len(h.versions), len(tt.wantVersions))
			}

			for version, entries := range h.versions {
				wantEntries := tt.wantVersions[version]
				if len(entries) != len(wantEntries) {
					t.Fatalf("storage.addVersion() entries length for version %v = %v, want %v", version, len(entries), len(wantEntries))
				}
				for i, entry := range entries {
					wantEntry := wantEntries[i]
					if entry.Path != wantEntry.Path {
						t.Errorf("storage.addVersion() entry Path for version %v = %v, want %v", version, entry.Path, wantEntry.Path)
					}
					if entry.Data.Format() != wantEntry.Data.Format() {
						t.Errorf("storage.addVersion() entry Data format for version %v = %v, want %v", version, entry.Data.Format(), wantEntry.Data.Format())
					}
					if entry.Data.Data() != wantEntry.Data.Data() {
						t.Errorf("storage.addVersion() entry Data data for version %v = %v, want %v", version, entry.Data.Data(), wantEntry.Data.Data())
					}
				}
			}
		})
	}
}
