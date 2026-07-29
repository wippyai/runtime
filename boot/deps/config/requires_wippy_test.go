// SPDX-License-Identifier: MPL-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModuleConfigRequiresWippy(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultConfigFile)
	require.NoError(t, os.WriteFile(path, []byte(`organization: acme
module: consumer
version: 1.0.0
requires_wippy: ">=1.2.0"
`), 0o644))
	cfg, err := LoadFrom(path)
	require.NoError(t, err)
	require.Equal(t, ">=1.2.0", cfg.RequiresWippy)
	require.NoError(t, cfg.ValidateRuntimeVersion("1.2.0"))
	require.NoError(t, cfg.ValidateRuntimeVersion("dev"))
	require.EqualError(t, cfg.ValidateRuntimeVersion("1.1.9"), "wippy 1.1.9 does not satisfy requires_wippy >=1.2.0")
}
