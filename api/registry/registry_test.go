package registry

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestID_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ID
		wantErr  bool
	}{
		{
			name:     "valid object format",
			input:    `{"ns":"test-ns","id":"test-name"}`,
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
			name:     "invalid object format - missing fields",
			input:    `{"ns":"test-ns"}`,
			expected: ID{},
			wantErr:  true,
		},
		{
			name:     "invalid json",
			input:    `{"ns":test-ns","id":"test-name"}`,
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
				assert.Equal(t, tt.expected, id)
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
				"id": "database.postgresql.primary"
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
				"id": "auth.jwt.secret@v2"
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
				assert.Equal(t, tt.expected, id)
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
			assert.Equal(t, tt.expected, result)
		})
	}
}
