// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestPublishCredentialRequirements(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		want    string
		dryRun  bool
	}{
		{"dry run requires explicit version", "", "version is required", true},
		{"upload requires credentials", "1.2.3", "not authenticated", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv("HOME", dir)
			t.Setenv("XDG_CONFIG_HOME", dir)
			t.Setenv("WIPPY_TOKEN", "")
			t.Setenv("WIPPY_REGISTRY", "https://hub.wippy.ai")
			if err := os.WriteFile(filepath.Join(dir, "wippy.yaml"), []byte("organization: test\nmodule: example\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			command := &cobra.Command{}
			command.Flags().String("config", dir, "")
			command.Flags().Bool("dry-run", tc.dryRun, "")
			command.Flags().String("version", tc.version, "")
			previousSilent := silentLogs
			t.Cleanup(func() { silentLogs = previousSilent })
			err := runPublish(command, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("runPublish error = %v, want %q", err, tc.want)
			}
		})
	}
}
