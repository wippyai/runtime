// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseModuleRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		org       string
		module    string
		version   string
		errSubstr string
		wantErr   bool
		isLabel   bool
	}{
		{
			name:    "no version",
			input:   "wippy/terminal",
			org:     "wippy",
			module:  "terminal",
			version: "",
			isLabel: false,
		},
		{
			name:    "plain semver",
			input:   "wippy/terminal@1.2.3",
			org:     "wippy",
			module:  "terminal",
			version: "1.2.3",
			isLabel: false,
		},
		{
			name:    "v-prefixed semver",
			input:   "wippy/terminal@v1.2.3",
			org:     "wippy",
			module:  "terminal",
			version: "v1.2.3",
			isLabel: false,
		},
		{
			name:    "semver pre-release",
			input:   "wippy/terminal@1.2.3-beta.1",
			org:     "wippy",
			module:  "terminal",
			version: "1.2.3-beta.1",
			isLabel: false,
		},
		{
			name:    "label latest",
			input:   "wippy/terminal@latest",
			org:     "wippy",
			module:  "terminal",
			version: "latest",
			isLabel: true,
		},
		{
			name:      "missing slash",
			input:     "wippy-terminal",
			wantErr:   true,
			errSubstr: "invalid module reference",
		},
		{
			name:      "missing module",
			input:     "wippy/",
			wantErr:   true,
			errSubstr: "invalid module reference",
		},
		{
			name:      "uppercase org rejected",
			input:     "Wippy/terminal",
			wantErr:   true,
			errSubstr: "invalid module reference",
		},
		{
			name:      "uppercase module rejected",
			input:     "wippy/Terminal",
			wantErr:   true,
			errSubstr: "invalid module reference",
		},
		{
			name:      "module cannot start with digit",
			input:     "wippy/1terminal",
			wantErr:   true,
			errSubstr: "invalid module reference",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ref, err := parseModuleRef(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				if tc.errSubstr != "" {
					assert.Contains(t, err.Error(), tc.errSubstr)
				}
				assert.Nil(t, ref)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, ref)
			assert.Equal(t, tc.org, ref.Org)
			assert.Equal(t, tc.module, ref.Module)
			assert.Equal(t, tc.version, ref.Version)
			assert.Equal(t, tc.isLabel, ref.IsLabel)
		})
	}
}

func TestIsValidSemver(t *testing.T) {
	t.Parallel()

	assert.True(t, isValidSemver("1.2.3"))
	assert.True(t, isValidSemver("v1.2.3"))
	assert.True(t, isValidSemver("1.2.3-beta.1"))
	assert.False(t, isValidSemver("latest"))
	assert.False(t, isValidSemver("1.2"))
	assert.False(t, isValidSemver("foo1.2.3"))
}
