package loader

//
//func TestRequirementsAndExports(t *testing.T) {
//	// Spawn a test transcoder that handles JSON and YAML
//	transcoder := tr.NewTranscoder()
//	json.Register(transcoder)
//	yaml.Register(transcoder)
//
//	tests := []struct {
//		name        string
//		input       string
//		format      payload.Format
//		exports     map[string]Export
//		want        []registry.Entry
//		wantErr     assert.ErrorAssertionFunc
//		wantExports map[string]Export
//	}{
//		{
//			name: "basic requirements with exports (JSON)",
//			input: `{
//				"namespace": "test-json",
//				"entries": [
//					{"name": "API_KEY", "kind": "ns.requirement", "targets": [{"entry": "api_service", "path": "config.auth.key"}]},
//					{"name": "api_service", "kind": "service"}
//				]
//			}`,
//			format: payload.JSON,
//			exports: map[string]Export{
//				"API_KEY": {Name: "API_KEY", Description: "System API key", Value: "secret-123"},
//			},
//			wantExports: map[string]Export{
//				"API_KEY": {Name: "API_KEY", Value: "secret-123"},
//			},
//			want: []registry.Entry{
//				{
//					ID:   registry.ID{NS: "test-json", Name: "API_KEY"},
//					Kind: "ns.requirement",
//					Data: payload.New(map[string]any{
//						"name": "API_KEY",
//						"kind": "ns.requirement",
//						"targets": []map[string]any{
//							{"entry": "api_service", "path": "config.auth.key"},
//						},
//					}),
//				},
//				{
//					ID:   registry.ID{NS: "test-json", Name: "api_service"},
//					Kind: "service",
//					Data: payload.New(map[string]any{
//						"name":   "api_service",
//						"kind":   "service",
//						"config": map[string]any{"auth": map[string]any{"key": "secret-123"}},
//					}),
//				},
//			},
//			wantErr: assert.NoError,
//		},
//		{
//			name: "basic requirements with exports (YAML)",
//			input: `
//namespace: test-yaml
//entries:
//  - name: DB_HOST
//    kind: ns.requirement
//    targets:
//      - entry: hello_world_dependency
//        path: namespace
//  - name: hello_world_dependency
//    kind: ns.dependency
//    namespace: "app.requirements.demo"
//    api_router: "system:api"
//  - name: db_connector
//    kind: database
//`,
//			format: payload.YAML,
//			exports: map[string]Export{
//				"DB_HOST": {Name: "DB_HOST", Value: "app.requirements.demo"},
//			},
//			want: []registry.Entry{
//				{
//					ID:   registry.ID{NS: "test-yaml", Name: "DB_HOST"},
//					Kind: "ns.requirement",
//					Data: payload.New(map[string]any{
//						"name": "DB_HOST",
//						"kind": "ns.requirement",
//						"targets": []map[string]any{
//							{"entry": "hello_world_dependency", "path": "namespace"},
//						},
//					}),
//				},
//				{
//					ID:   registry.ID{NS: "test-yaml", Name: "hello_world_dependency"},
//					Kind: "ns.dependency",
//					Data: payload.New(map[string]any{
//						"name":       "hello_world_dependency",
//						"kind":       "ns.dependency",
//						"namespace":  "app.requirements.demo",
//						"api_router": "system:api",
//					}),
//				},
//				{
//					ID:   registry.ID{NS: "test-yaml", Name: "db_connector"},
//					Kind: "database",
//					Data: payload.New(map[string]any{
//						"name": "db_connector",
//						"kind": "database",
//					}),
//				},
//			},
//			wantErr: assert.NoError,
//			wantExports: map[string]Export{
//				"DB_HOST": {Name: "DB_HOST", Value: "app.requirements.demo"},
//			},
//		},
//		{
//			name: "requirement not satisfied",
//			input: `{
//				"namespace": "test-err",
//				"entries": [
//					{"name": "MISSING_PARAM", "kind": "ns.requirement", "targets": [{"entry": "service", "path": "config.key"}]},
//					{"name": "service", "kind": "generic"}
//				]
//			}`,
//			format: payload.JSON,
//			exports: map[string]Export{
//				"OTHER_PARAM": {Value: "some-value"},
//			},
//			want:        nil,
//			wantErr:     assert.Error,
//			wantExports: map[string]Export{},
//		},
//		{
//			name: "requirement satisfied but not targeted for namespace",
//			input: `{
//				"namespace": "target-ns",
//				"entries": [
//					{"name": "TARGETED_EXPORT", "kind": "ns.requirement", "targets": [{"entry": "s1", "path": "val"}]},
//					{"name": "s1", "kind": "k1"}
//				]
//			}`,
//			format: payload.JSON,
//			exports: map[string]Export{
//				"TARGETED_EXPORT": {Name: "TARGETED_EXPORT", Value: "export-val", Targets: []string{"other-ns"}}, // Export targeted to 'other-ns'
//			},
//			want:        nil,
//			wantErr:     assert.Error,
//			wantExports: map[string]Export{},
//		},
//		{
//			name: "requirement satisfied with correct namespace target",
//			input: `{
//				"namespace": "correct-ns",
//				"entries": [
//					{"name": "NS_TARGETED", "kind": "ns.requirement", "targets": [{"entry": "s2", "path": "val"}]},
//					{"name": "s2", "kind": "k2"}
//				]
//			}`,
//			format: payload.JSON,
//			exports: map[string]Export{
//				"NS_TARGETED": {Name: "NS_TARGETED", Value: "ns-export-val", Targets: []string{"correct-ns", "another-ns"}}, // Export allows 'correct-ns'
//			},
//			want: []registry.Entry{
//				{
//					ID:   registry.ID{NS: "correct-ns", Name: "NS_TARGETED"},
//					Kind: "ns.requirement",
//					Data: payload.New(map[string]any{
//						"name": "NS_TARGETED",
//						"kind": "ns.requirement",
//						"targets": []map[string]any{
//							{"entry": "s2", "path": "val"},
//						},
//					}),
//				},
//				{
//					ID:   registry.ID{NS: "correct-ns", Name: "s2"},
//					Kind: "k2",
//					Data: payload.New(map[string]any{
//						"name": "s2",
//						"kind": "k2",
//						"val":  "ns-export-val",
//					}),
//				},
//			},
//			wantErr: assert.NoError,
//			wantExports: map[string]Export{
//				"NS_TARGETED": {Name: "NS_TARGETED", Value: "ns-export-val"},
//			},
//		},
//		{
//			name: "requirement with wildcard target name",
//			input: `{
//				"namespace": "wildcard-ns",
//				"entries": [
//					{"name": "WILD_PARAM", "kind": "ns.requirement", "targets": [{"path": "config.wild"}]},
//					{"name": "service-a", "kind": "type-a"},
//					{"name": "service-b", "kind": "type-b"}
//				]
//			}`,
//			format: payload.JSON,
//			exports: map[string]Export{
//				"WILD_PARAM": {Value: "wild-value"},
//			},
//			wantExports: map[string]Export{
//				"WILD_PARAM": {Name: "WILD_PARAM", Value: "wild-value"},
//			},
//			want: []registry.Entry{
//				{
//					ID:   registry.ID{NS: "wildcard-ns", Name: "WILD_PARAM"},
//					Kind: "ns.requirement",
//					Data: payload.New(map[string]any{
//						"name": "WILD_PARAM",
//						"kind": "ns.requirement",
//						"targets": []map[string]any{
//							{"path": "config.wild"},
//						},
//					}),
//				},
//				{
//					ID:   registry.ID{NS: "wildcard-ns", Name: "service-a"},
//					Kind: "type-a",
//					Data: payload.New(map[string]any{
//						"name":   "service-a",
//						"kind":   "type-a",
//						"config": map[string]any{"wild": "wild-value"},
//					}),
//				},
//				{
//					ID:   registry.ID{NS: "wildcard-ns", Name: "service-b"},
//					Kind: "type-b",
//					Data: payload.New(map[string]any{
//						"name":   "service-b",
//						"kind":   "type-b",
//						"config": map[string]any{"wild": "wild-value"},
//					}),
//				},
//			},
//			wantErr: assert.NoError,
//		},
//		{
//			name: "requirement targeting specific entry and wildcard",
//			input: `{
//				"namespace": "multi-target",
//				"entries": [
//					{"name": "MULTI_PARAM", "kind": "ns.requirement", "targets": [
//						{"entry": "specific-svc", "path": "specific_key"},
//						{"path": "common_key"}
//					]},
//					{"name": "specific-svc", "kind": "special"},
//					{"name": "other-svc", "kind": "generic"}
//				]
//			}`,
//			format: payload.JSON,
//			exports: map[string]Export{
//				"MULTI_PARAM": {Value: "multi-value"},
//			},
//			wantExports: map[string]Export{
//				"MULTI_PARAM": {Name: "MULTI_PARAM", Value: "multi-value"},
//			},
//			want: []registry.Entry{
//				{
//					ID:   registry.ID{NS: "multi-target", Name: "MULTI_PARAM"},
//					Kind: "ns.requirement",
//					Data: payload.New(map[string]any{
//						"name": "MULTI_PARAM",
//						"kind": "ns.requirement",
//						"targets": []map[string]any{
//							{"entry": "specific-svc", "path": "specific_key"},
//							{"path": "common_key"},
//						},
//					}),
//				},
//				{
//					ID:   registry.ID{NS: "multi-target", Name: "specific-svc"},
//					Kind: "special",
//					Data: payload.New(map[string]any{
//						"name":         "specific-svc",
//						"kind":         "special",
//						"specific_key": "multi-value",
//						"common_key":   "multi-value",
//					}),
//				},
//				{
//					ID:   registry.ID{NS: "multi-target", Name: "other-svc"},
//					Kind: "generic",
//					Data: payload.New(map[string]any{
//						"name":       "other-svc",
//						"kind":       "generic",
//						"common_key": "multi-value",
//					}),
//				},
//			},
//			wantErr: assert.NoError,
//		},
//		{
//			name: "dependency-based requirement resolution",
//			input: `{
//				"namespace": "dependency-test",
//				"entries": [
//					{"name": "NAMESPACE", "kind": "ns.requirement", "targets": [{"entry": "hello_world_dependency", "path": "namespace"}]},
//					{"name": "API_ROUTER", "kind": "ns.requirement", "targets": [{"entry": "hello_world_dependency", "path": "api_router"}]},
//					{"name": "hello_world_dependency", "kind": "ns.dependency", "namespace": "app.requirements.demo", "api_router": "system:api"},
//					{"name": "service_handler", "kind": "function.lua", "source": "file://handler.lua"}
//				]
//			}`,
//			format: payload.JSON,
//			exports: map[string]Export{
//				"NAMESPACE":  {Name: "NAMESPACE", Value: "app.requirements.demo"},
//				"API_ROUTER": {Name: "API_ROUTER", Value: "system:api"},
//			},
//			want: []registry.Entry{
//				{
//					ID:   registry.ID{NS: "dependency-test", Name: "NAMESPACE"},
//					Kind: "ns.requirement",
//					Data: payload.New(map[string]any{
//						"name": "NAMESPACE",
//						"kind": "ns.requirement",
//						"targets": []map[string]any{
//							{"entry": "hello_world_dependency", "path": "namespace"},
//						},
//					}),
//				},
//				{
//					ID:   registry.ID{NS: "dependency-test", Name: "API_ROUTER"},
//					Kind: "ns.requirement",
//					Data: payload.New(map[string]any{
//						"name": "API_ROUTER",
//						"kind": "ns.requirement",
//						"targets": []map[string]any{
//							{"entry": "hello_world_dependency", "path": "api_router"},
//						},
//					}),
//				},
//				{
//					ID:   registry.ID{NS: "dependency-test", Name: "hello_world_dependency"},
//					Kind: "ns.dependency",
//					Data: payload.New(map[string]any{
//						"name":       "hello_world_dependency",
//						"kind":       "ns.dependency",
//						"namespace":  "app.requirements.demo",
//						"api_router": "system:api",
//					}),
//				},
//				{
//					ID:   registry.ID{NS: "dependency-test", Name: "service_handler"},
//					Kind: "function.lua",
//					Data: payload.New(map[string]any{
//						"name":   "service_handler",
//						"kind":   "function.lua",
//						"source": "file://handler.lua",
//					}),
//				},
//			},
//			wantErr: assert.NoError,
//			wantExports: map[string]Export{
//				"NAMESPACE":  {Name: "NAMESPACE", Value: "app.requirements.demo"},
//				"API_ROUTER": {Name: "API_ROUTER", Value: "system:api"},
//			},
//		},
//		{
//			name: "old-style requirements (module declaration)",
//			input: `{
//				"namespace": "module-test",
//				"requirements": [
//					{
//						"parameter": "API_KEY",
//						"description": "API key for external service",
//						"targets": [
//							{"value": "config.api.key"}
//						]
//					},
//					{
//						"parameter": "NAMESPACE",
//						"description": "Target namespace",
//						"targets": [
//							{"value": "meta.namespace"}
//						]
//					}
//				],
//				"entries": [
//					{"name": "service_handler", "kind": "function.lua", "source": "file://handler.lua"}
//				]
//			}`,
//			format: payload.JSON,
//			exports: map[string]Export{
//				"API_KEY":   {Name: "API_KEY", Value: "secret-api-key-123"},
//				"NAMESPACE": {Name: "NAMESPACE", Value: "app.module.test"},
//			},
//			want: []registry.Entry{
//				{
//					ID:   registry.ID{NS: "module-test", Name: "service_handler"},
//					Kind: "function.lua",
//					Data: payload.New(map[string]any{
//						"name":   "service_handler",
//						"kind":   "function.lua",
//						"source": "file://handler.lua",
//						"config": map[string]any{"api": map[string]any{"key": "secret-api-key-123"}},
//						"meta":   map[string]any{"namespace": "app.module.test"},
//					}),
//				},
//			},
//			wantErr: assert.NoError,
//			wantExports: map[string]Export{
//				"API_KEY":   {Name: "API_KEY", Value: "secret-api-key-123"},
//				"NAMESPACE": {Name: "NAMESPACE", Value: "app.module.test"},
//			},
//		},
//	}
//
//	for _, tt := range tests {
//		t.Run(tt.name, func(t *testing.T) {
//			p := payload.NewPayload([]byte(tt.input), tt.format)
//
//			got, exports, err := ExtractEntries(p, transcoder, tt.exports)
//			if !tt.wantErr(t, err, "ExtractEntries() error = %v, wantErr %v", err, tt.wantErr != nil) {
//				return
//			}
//
//			assert.Subsetf(t, tt.wantExports, exports, "ExtractEntries() exports = %v, want %v", exports, tt.wantExports)
//
//			gotJSON := must(stdjson.MarshalIndent(got, "", "  "))
//			wantJSON := must(stdjson.MarshalIndent(tt.want, "", "  "))
//
//			assert.JSONEqf(t, string(gotJSON), string(wantJSON), "ExtractEntries() = \n%s\n, want \n%s", string(gotJSON), string(wantJSON))
//		})
//	}
//}
//
//func TestExtractEntries(t *testing.T) {
//	// Spawn a test transcoder that handles JSON
//	transcoder := tr.NewTranscoder()
//	jsonRegister := json.Register
//	jsonRegister(transcoder)
//
//	tests := []struct {
//		name    string
//		input   string
//		want    []registry.Entry
//		wantErr bool
//	}{
//		{
//			name: "single entry case",
//			input: `{
//				"namespace": "test",
//				"name": "single-entry",
//				"kind": "service",
//				"meta": {
//					"version": "1.0",
//					"tags": ["test", "service"]
//				},
//				"url": "http://example.com",
//				"port": 8080
//			}`,
//			want: []registry.Entry{
//				{
//					ID: registry.ID{
//						NS:   "test",
//						Name: "single-entry",
//					},
//					Kind: "service",
//					Meta: registry.Metadata{
//						"version": "1.0",
//						"tags":    []interface{}{"test", "service"},
//					},
//					Data: payload.NewPayload(map[string]interface{}{
//						"namespace": "test",
//						"name":      "single-entry",
//						"kind":      "service",
//						"meta": map[string]interface{}{
//							"version": "1.0",
//							"tags":    []interface{}{"test", "service"},
//						},
//						"url":  "http://example.com",
//						"port": float64(8080),
//					}, payload.JSON),
//				},
//			},
//			wantErr: false,
//		},
//		{
//			name: "batch entries case",
//			input: `{
//				"namespace": "test",
//				"meta": {
//					"shared": "value"
//				},
//				"entries": [
//					{
//						"name": "entry1",
//						"kind": "service",
//						"meta": {
//							"version": "1.0"
//						},
//						"data": {
//							"url": "http://example1.com"
//						}
//					},
//					{
//						"name": "entry2",
//						"kind": "endpoint",
//						"meta": {
//							"version": "2.0"
//						},
//						"data": {
//							"path": "/api/v2"
//						}
//					}
//				]
//			}`,
//			want: []registry.Entry{
//				{
//					ID: registry.ID{
//						NS:   "test",
//						Name: "entry1",
//					},
//					Kind: "service",
//					Meta: registry.Metadata{
//						"version": "1.0",
//						"shared":  "value",
//					},
//					Data: payload.New(map[string]interface{}{
//						"data": map[string]interface{}{
//							"url": "http://example1.com",
//						},
//					}),
//				},
//				{
//					ID: registry.ID{
//						NS:   "test",
//						Name: "entry2",
//					},
//					Kind: "endpoint",
//					Meta: registry.Metadata{
//						"version": "2.0",
//						"shared":  "value",
//					},
//					Data: payload.New(map[string]interface{}{
//						"data": map[string]interface{}{
//							"path": "/api/v2",
//						},
//					}),
//				},
//			},
//			wantErr: false,
//		},
//		{
//			name: "missing namespace",
//			input: `{
//				"name": "test",
//				"kind": "service"
//			}`,
//			want:    nil,
//			wantErr: true,
//		},
//		// {
//		// 	name: "invalid JSON",
//		// 	input: `{
//		// 		"namespace": "test"
//		// 		"invalid": json,
//		// 	}`,
//		// 	want:    nil,
//		// 	wantErr: true,
//		// },
//		{
//			name: "empty metadata in batch entry",
//			input: `{
//				"namespace": "test",
//				"entries": [
//					{
//						"name": "entry1",
//						"kind": "service",
//						"meta": null,
//						"data": {"url": "http://example.com"}
//					}
//				]
//			}`,
//			want: []registry.Entry{
//				{
//					ID: registry.ID{
//						NS:   "test",
//						Name: "entry1",
//					},
//					Kind: "service",
//					Data: payload.New(map[string]interface{}{
//						"data": map[string]interface{}{
//							"url": "http://example.com",
//						},
//					}),
//				},
//			},
//			wantErr: false,
//		},
//		{
//			name: "entry with missing required fields",
//			input: `{
//				"namespace": "test",
//				"entries": [
//					{
//						"data": {"url": "http://example.com"}
//					}
//				]
//			}`,
//			want:    nil,
//			wantErr: true,
//		},
//		{
//			name: "complex metadata types",
//			input: `{
//                "namespace": "test",
//                "meta": {
//                    "numbers": [1, 2, 3],
//                    "nested": {"key": "value"},
//                    "bool": true
//                },
//                "entries": [
//                    {
//                        "name": "entry1",
//                        "kind": "service",
//                        "meta": {
//                            "arrays": ["a", "b"],
//                            "numbers": [4, 5, 6]
//                        },
//                        "data": {"url": "http://example.com"}
//                    }
//                ]
//            }`,
//			want: []registry.Entry{
//				{
//					ID: registry.ID{
//						NS:   "test",
//						Name: "entry1",
//					},
//					Kind: "service",
//					Meta: registry.Metadata{
//						"arrays":  []interface{}{"a", "b"},
//						"numbers": []interface{}{4, 5, 6},
//						"nested":  map[string]interface{}{"key": "value"},
//						"bool":    true,
//					},
//					Data: payload.New(map[string]interface{}{
//						"data": map[string]interface{}{
//							"url": "http://example.com",
//						},
//					}),
//				},
//			},
//			wantErr: false,
//		},
//		{
//			name: "empty entries array",
//			input: `{
//				"namespace": "test",
//				"entries": []
//			}`,
//			want:    []registry.Entry{},
//			wantErr: false,
//		},
//	}
//
//	for _, tt := range tests {
//		t.Run(tt.name, func(t *testing.T) {
//			// Spawn JSON payload
//			p := payload.NewPayload(tt.input, payload.JSON)
//
//			got, _, err := ExtractEntries(p, transcoder, nil)
//			if (err != nil) != tt.wantErr {
//				t.Errorf("ExtractEntries() error = %v, wantErr %v", err, tt.wantErr)
//				return
//			}
//
//			if !tt.wantErr {
//				if len(got) != len(tt.want) {
//					t.Errorf("ExtractEntries() got %d entries, want %d", len(got), len(tt.want))
//					return
//				}
//
//				for i := range got {
//					// Check Process
//					if !reflect.DeepEqual(got[i].ID, tt.want[i].ID) {
//						t.Errorf("Entry[%d].Process = %v, want %v", i, got[i].ID, tt.want[i].ID)
//					}
//
//					// Check Kind
//					if got[i].Kind != tt.want[i].Kind {
//						t.Errorf("Entry[%d].Kind = %v, want %v", i, got[i].Kind, tt.want[i].Kind)
//					}
//
//					// Check Meta
//					if !equalMetadata(got[i].Meta, tt.want[i].Meta) {
//						t.Errorf("Entry[%d].Meta = %+v, want %+v", i, got[i].Meta, tt.want[i].Meta)
//					}
//
//					// For Data, check format and ensure it's not nil
//					if got[i].Data == nil {
//						t.Errorf("Entry[%d].Data is nil", i)
//						continue
//					}
//				}
//			}
//		})
//	}
//}
//
//func TestMergeMeta(t *testing.T) {
//	tests := []struct {
//		name     string
//		base     registry.Metadata
//		override registry.Metadata
//		want     registry.Metadata
//	}{
//		{
//			name: "override string slices",
//			base: registry.Metadata{
//				"tags":    []string{"base1", "base2"},
//				"version": "1.0",
//			},
//			override: registry.Metadata{
//				"tags": []string{"override1", "base1"},
//				"env":  "prod",
//			},
//			want: registry.Metadata{
//				"tags":    []string{"override1", "base1"}, // Override completely
//				"version": "1.0",
//				"env":     "prod",
//			},
//		},
//		{
//			name: "merge with nil values",
//			base: registry.Metadata{
//				"key": "value",
//			},
//			override: nil,
//			want: registry.Metadata{
//				"key": "value",
//			},
//		},
//		{
//			name: "merge empty override",
//			base: nil,
//			override: registry.Metadata{
//				"key": "value",
//			},
//			want: registry.Metadata{
//				"key": "value",
//			},
//		},
//		{
//			name: "merge with interface slices",
//			base: registry.Metadata{
//				"tags": []interface{}{"base1", "base2"},
//			},
//			override: registry.Metadata{
//				"tags": []interface{}{"override1"},
//			},
//			want: registry.Metadata{
//				"tags": []interface{}{"override1"}, // Notify replacement
//			},
//		},
//	}
//
//	for _, tt := range tests {
//		t.Run(tt.name, func(t *testing.T) {
//			got := mergeMeta(tt.base, tt.override)
//			if !reflect.DeepEqual(got, tt.want) {
//				t.Errorf("mergeMeta() = %v, want %v", got, tt.want)
//			}
//		})
//	}
//}
//
//func TestMetadataMergingInData(t *testing.T) {
//	// Spawn a test transcoder that handles JSON
//	transcoder := tr.NewTranscoder()
//	jsonRegister := json.Register
//	jsonRegister(transcoder)
//
//	tests := []struct {
//		name    string
//		input   string
//		want    []registry.Entry
//		wantErr bool
//	}{
//		{
//			name: "metadata should merge into data field",
//			input: `{
//                "namespace": "test",
//                "meta": {
//                    "server": "system:gateway",
//                    "router": "system:router",
//                    "depends_on": ["ns:functions", "ns:system"]
//                },
//                "entries": [
//                    {
//                        "name": "api.endpoint",
//                        "kind": "http.endpoint",
//                        "meta": {
//                            "comment": "Test endpoint"
//                        },
//                        "method": "GET",
//                        "path": "/test",
//                        "handler": "functions:test.handler"
//                    }
//                ]
//            }`,
//			want: []registry.Entry{
//				{
//					ID: registry.ID{
//						NS:   "test",
//						Name: "api.endpoint",
//					},
//					Kind: "http.endpoint",
//					Meta: registry.Metadata{
//						"comment":    "Test endpoint",
//						"server":     "system:gateway",
//						"router":     "system:router",
//						"depends_on": []interface{}{"ns:functions", "ns:system"},
//					},
//					Data: payload.New(map[string]interface{}{
//						"meta": map[string]interface{}{
//							"comment":    "Test endpoint",
//							"server":     "system:gateway",
//							"router":     "system:router",
//							"depends_on": []interface{}{"ns:functions", "ns:system"},
//						},
//						"method":  "GET",
//						"path":    "/test",
//						"handler": "functions:test.handler",
//					}),
//				},
//			},
//			wantErr: false,
//		},
//	}
//
//	for _, tt := range tests {
//		t.Run(tt.name, func(t *testing.T) {
//			// Spawn JSON payload
//			p := payload.NewPayload(tt.input, payload.JSON)
//
//			got, _, err := ExtractEntries(p, transcoder, nil)
//			if (err != nil) != tt.wantErr {
//				t.Errorf("ExtractEntries() error = %v, wantErr %v", err, tt.wantErr)
//				return
//			}
//
//			if !tt.wantErr {
//				if len(got) != len(tt.want) {
//					t.Errorf("ExtractEntries() got %d entries, want %d", len(got), len(tt.want))
//					return
//				}
//
//				for i := range got {
//					// Check Process
//					if !reflect.DeepEqual(got[i].ID, tt.want[i].ID) {
//						t.Errorf("Entry[%d].Process = %v, want %v", i, got[i].ID, tt.want[i].ID)
//					}
//
//					// Check Kind
//					if got[i].Kind != tt.want[i].Kind {
//						t.Errorf("Entry[%d].Kind = %v, want %v", i, got[i].Kind, tt.want[i].Kind)
//					}
//
//					// Check Meta
//					if !equalMetadata(got[i].Meta, tt.want[i].Meta) {
//						t.Errorf("Entry[%d].Meta = %+v, want %+v", i, got[i].Meta, tt.want[i].Meta)
//					}
//
//					// Check Data content including metadata
//					gotData := make(map[string]interface{})
//					if err := transcoder.Unmarshal(got[i].Data, &gotData); err != nil {
//						t.Errorf("Failed to unmarshal got data: %v", err)
//						continue
//					}
//
//					wantData := make(map[string]interface{})
//					if err := transcoder.Unmarshal(tt.want[i].Data, &wantData); err != nil {
//						t.Errorf("Failed to unmarshal want data: %v", err)
//						continue
//					}
//
//					// Check if metadata is properly merged in data
//					gotMeta, ok := gotData["meta"].(map[string]interface{})
//					if !ok {
//						t.Errorf("Entry[%d].Data.meta is not a map", i)
//						continue
//					}
//
//					wantMeta, ok := wantData["meta"].(map[string]interface{})
//					if !ok {
//						t.Errorf("Want Entry[%d].Data.meta is not a map", i)
//						continue
//					}
//
//					if !equalMetadata(registry.Metadata(gotMeta), registry.Metadata(wantMeta)) {
//						t.Errorf("Entry[%d].Data.meta = %+v, want %+v", i, gotMeta, wantMeta)
//					}
//				}
//			}
//		})
//	}
//}
//
//// equalMetadata compares metadata contents while being lenient with numeric types
//func equalMetadata(got, want registry.Metadata) bool {
//	if len(got) != len(want) {
//		return false
//	}
//
//	for k, wantV := range want {
//		gotV, exists := got[k]
//		if !exists {
//			return false
//		}
//
//		// Handle slices specially
//		if wantSlice, ok := wantV.([]interface{}); ok {
//			gotSlice, ok := gotV.([]interface{})
//			if !ok || len(gotSlice) != len(wantSlice) {
//				return false
//			}
//			// Compare slice elements
//			for i := range wantSlice {
//				if !equalValue(gotSlice[i], wantSlice[i]) {
//					return false
//				}
//			}
//			continue
//		}
//
//		// For non-slice values
//		if !equalValue(gotV, wantV) {
//			return false
//		}
//	}
//	return true
//}
//
//// equalValue compares values while being lenient with numeric types
//func equalValue(got, want interface{}) bool {
//	// If they're directly equal, no need for special handling
//	if reflect.DeepEqual(got, want) {
//		return true
//	}
//
//	// Handle numeric comparisons
//	switch w := want.(type) {
//	case int:
//		if g, ok := got.(float64); ok {
//			return float64(w) == g
//		}
//	case float64:
//		if g, ok := got.(int); ok {
//			return w == float64(g)
//		}
//	case map[string]interface{}:
//		if g, ok := got.(map[string]interface{}); ok {
//			return equalMetadata(registry.Metadata(g), registry.Metadata(w))
//		}
//	}
//
//	return false
//}
//
//func must[E any](v E, err error) E {
//	if err != nil {
//		panic(err)
//	}
//	return v
//}
