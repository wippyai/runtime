// SPDX-License-Identifier: MPL-2.0

// Package registry provides service registry and entry management.
package registry

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// assertIDEqual compares IDs by their NS and Name fields only (ignoring cached str)
func assertIDEqual(t *testing.T, expected, actual ID) {
	t.Helper()
	assert.Equal(t, expected.NS, actual.NS, "NS mismatch")
	assert.Equal(t, expected.Name, actual.Name, "Name mismatch")
}

func TestID_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ID
		wantErr  bool
	}{
		{
			name:     "valid object format",
			input:    `{"ns":"test-ns","name":"test-name"}`,
			expected: ID{NS: "test-ns", Name: "test-name"},
			wantErr:  false,
		},
		{
			name:     "valid string format",
			input:    `"test-ns:test-name"`,
			expected: ID{NS: "test-ns", Name: "test-name"},
			wantErr:  false,
		},
		{
			name:     "name-only format",
			input:    `"test-name"`,
			expected: ID{NS: "", Name: "test-name"},
			wantErr:  false,
		},
		{
			name:     "invalid string format - missing colon",
			input:    `"test-ns-test-name"`,
			expected: ID{NS: "", Name: "test-ns-test-name"},
			wantErr:  false,
		},
		{
			name:     "invalid string format - empty namespace",
			input:    `":test-name"`,
			expected: ID{NS: "", Name: "test-name"},
			wantErr:  false,
		},
		{
			name:     "invalid string format - empty name",
			input:    `"test-ns:"`,
			expected: ID{NS: "test-ns", Name: ""},
			wantErr:  false,
		},
		{
			name:     "object format with missing name field",
			input:    `{"ns":"test-ns"}`,
			expected: ID{NS: "test-ns", Name: ""},
			wantErr:  false,
		},
		{
			name:     "invalid json",
			input:    `{"ns":test-ns","name":"test-name"}`,
			expected: ID{},
			wantErr:  true,
		},
		{
			name:     "complex namespace and name",
			input:    `"my.complex.namespace:my/complex/name"`,
			expected: ID{NS: "my.complex.namespace", Name: "my/complex/name"},
			wantErr:  false,
		},
		{
			name:     "complex name only",
			input:    `"my/complex/name/with/slashes"`,
			expected: ID{NS: "", Name: "my/complex/name/with/slashes"},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var id ID
			err := json.Unmarshal([]byte(tt.input), &id)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assertIDEqual(t, tt.expected, id)
			}
		})
	}
}

// TestID_UnmarshalJSON_RealWorld tests the UnmarshalJSON method with more realistic examples
func TestID_UnmarshalJSON_RealWorld(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ID
		wantErr  bool
	}{
		{
			name: "service configuration",
			input: `{
				"ns": "services",
				"name": "database.postgresql.primary"
			}`,
			expected: ID{
				NS:   "services",
				Name: "database.postgresql.primary",
			},
			wantErr: false,
		},
		{
			name:  "endpoint configuration",
			input: `"endpoints:api/v1/users"`,
			expected: ID{
				NS:   "endpoints",
				Name: "api/v1/users",
			},
			wantErr: false,
		},
		{
			name: "configuration with special characters",
			input: `{
				"ns": "config.prod-env",
				"name": "auth.jwt.secret@v2"
			}`,
			expected: ID{
				NS:   "config.prod-env",
				Name: "auth.jwt.secret@v2",
			},
			wantErr: false,
		},
		{
			name:  "name-only configuration",
			input: `"auth.jwt.secret@v2"`,
			expected: ID{
				NS:   "",
				Name: "auth.jwt.secret@v2",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var id ID
			err := json.Unmarshal([]byte(tt.input), &id)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assertIDEqual(t, tt.expected, id)
			}
		})
	}
}

func TestParseID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ID
	}{
		{
			name:     "namespace and name",
			input:    "test-ns:test-name",
			expected: ID{NS: "test-ns", Name: "test-name"},
		},
		{
			name:     "name only",
			input:    "test-name",
			expected: ID{NS: "", Name: "test-name"},
		},
		{
			name:     "empty namespace",
			input:    ":test-name",
			expected: ID{NS: "", Name: "test-name"},
		},
		{
			name:     "empty name",
			input:    "test-ns:",
			expected: ID{NS: "test-ns", Name: ""},
		},
		{
			name:     "complex namespace and name",
			input:    "my.complex.namespace:my/complex/name",
			expected: ID{NS: "my.complex.namespace", Name: "my/complex/name"},
		},
		{
			name:     "complex name only",
			input:    "my/complex/name/with/slashes",
			expected: ID{NS: "", Name: "my/complex/name/with/slashes"},
		},
		{
			name:     "multiple colons",
			input:    "ns:name:with:colons",
			expected: ID{NS: "ns", Name: "name:with:colons"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: ID{NS: "", Name: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseID(tt.input)
			assertIDEqual(t, tt.expected, result)
		})
	}
}

func TestID_String(t *testing.T) {
	tests := []struct {
		name     string
		id       ID
		expected string
	}{
		{
			name:     "with namespace and name",
			id:       ID{NS: "test-ns", Name: "test-name"},
			expected: "test-ns:test-name",
		},
		{
			name:     "name only",
			id:       ID{NS: "", Name: "test-name"},
			expected: ":test-name",
		},
		{
			name:     "namespace only",
			id:       ID{NS: "test-ns", Name: ""},
			expected: "test-ns:",
		},
		{
			name:     "empty ID",
			id:       ID{NS: "", Name: ""},
			expected: ":",
		},
		{
			name:     "complex namespace and name",
			id:       ID{NS: "my.namespace", Name: "my/name"},
			expected: "my.namespace:my/name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.id.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestID_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		id       ID
		expected string
	}{
		{
			name:     "with namespace and name",
			id:       ID{NS: "test-ns", Name: "test-name"},
			expected: `"test-ns:test-name"`,
		},
		{
			name:     "name only",
			id:       ID{NS: "", Name: "test-name"},
			expected: `"test-name"`,
		},
		{
			name:     "complex namespace and name",
			id:       ID{NS: "my.namespace", Name: "my/name"},
			expected: `"my.namespace:my/name"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := json.Marshal(&tt.id)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, string(result))
		})
	}
}

func TestID_WithDefaultNS(t *testing.T) {
	tests := []struct {
		name      string
		id        ID
		defaultNS Namespace
		expected  ID
	}{
		{
			name:      "id without namespace",
			id:        ID{NS: "", Name: "test-name"},
			defaultNS: "default-ns",
			expected:  ID{NS: "default-ns", Name: "test-name"},
		},
		{
			name:      "id with namespace",
			id:        ID{NS: "existing-ns", Name: "test-name"},
			defaultNS: "default-ns",
			expected:  ID{NS: "existing-ns", Name: "test-name"},
		},
		{
			name:      "empty default namespace",
			id:        ID{NS: "", Name: "test-name"},
			defaultNS: "",
			expected:  ID{NS: "", Name: "test-name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.id.WithDefaultNS(tt.defaultNS)
			assertIDEqual(t, tt.expected, result)
		})
	}
}

func TestID_Equal(t *testing.T) {
	tests := []struct {
		name     string
		id1      ID
		id2      ID
		expected bool
	}{
		{
			name:     "equal IDs",
			id1:      ID{NS: "ns", Name: "name"},
			id2:      ID{NS: "ns", Name: "name"},
			expected: true,
		},
		{
			name:     "different namespaces",
			id1:      ID{NS: "ns1", Name: "name"},
			id2:      ID{NS: "ns2", Name: "name"},
			expected: false,
		},
		{
			name:     "different names",
			id1:      ID{NS: "ns", Name: "name1"},
			id2:      ID{NS: "ns", Name: "name2"},
			expected: false,
		},
		{
			name:     "both empty",
			id1:      ID{},
			id2:      ID{},
			expected: true,
		},
		{
			name:     "one empty",
			id1:      ID{NS: "ns", Name: "name"},
			id2:      ID{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.id1.Equal(tt.id2)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewID(t *testing.T) {
	tests := []struct {
		name     string
		ns       string
		idName   string
		expected ID
	}{
		{
			name:     "normal ID",
			ns:       "test-ns",
			idName:   "test-name",
			expected: ID{NS: "test-ns", Name: "test-name"},
		},
		{
			name:     "empty namespace",
			ns:       "",
			idName:   "test-name",
			expected: ID{NS: "", Name: "test-name"},
		},
		{
			name:     "empty name",
			ns:       "test-ns",
			idName:   "",
			expected: ID{NS: "test-ns", Name: ""},
		},
		{
			name:     "both empty",
			ns:       "",
			idName:   "",
			expected: ID{NS: "", Name: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewID(tt.ns, tt.idName)
			assertIDEqual(t, tt.expected, result)
			assert.Equal(t, tt.ns+":"+tt.idName, result.String())
		})
	}
}

func TestID_String_WithCachedStr(t *testing.T) {
	id := NewID("ns", "name")
	assert.Equal(t, "ns:name", id.String())

	id2 := ParseID("ns:name")
	assert.Equal(t, "ns:name", id2.String())
}
