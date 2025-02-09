package registry

import (
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"testing"
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
			name:     "invalid string format - missing colon",
			input:    `"test-ns-test-name"`,
			expected: ID{},
			wantErr:  true,
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
