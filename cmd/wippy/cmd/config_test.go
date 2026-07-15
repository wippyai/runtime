// SPDX-License-Identifier: MPL-2.0

package cmd

import "testing"

func setTestConfigFiles(t *testing.T, paths ...string) {
	t.Helper()
	previous := configFiles
	configFiles = append([]string(nil), paths...)
	t.Cleanup(func() {
		configFiles = previous
	})
}

func TestRuntimeConfigPaths(t *testing.T) {
	t.Run("uses optional default when flag is absent", func(t *testing.T) {
		setTestConfigFiles(t)
		paths := runtimeConfigPaths()
		if len(paths) != 1 || paths[0] != defaultConfigFile {
			t.Fatalf("runtimeConfigPaths() = %v, want [%q]", paths, defaultConfigFile)
		}
	})

	t.Run("preserves every explicit path in order", func(t *testing.T) {
		setTestConfigFiles(t, "base.yaml", "dev.yaml", "workspace.yaml")
		paths := runtimeConfigPaths()
		want := []string{"base.yaml", "dev.yaml", "workspace.yaml"}
		if len(paths) != len(want) {
			t.Fatalf("runtimeConfigPaths() = %v, want %v", paths, want)
		}
		for i := range want {
			if paths[i] != want[i] {
				t.Fatalf("runtimeConfigPaths()[%d] = %q, want %q", i, paths[i], want[i])
			}
		}

		paths[0] = "mutated.yaml"
		if configFiles[0] != "base.yaml" {
			t.Fatal("runtimeConfigPaths returned the mutable global slice")
		}
	})
}

func TestRuntimeConfigFlagIsRepeatable(t *testing.T) {
	flag := rootCmd.PersistentFlags().Lookup("config")
	if flag == nil {
		t.Fatal("missing --config flag")
	}
	if got := flag.Value.Type(); got != "stringArray" {
		t.Fatalf("--config flag type = %q, want stringArray", got)
	}
}

func TestPublishConfigFlagRemainsManifestDirectory(t *testing.T) {
	flag := publishCmd.Flags().Lookup("config")
	if flag == nil {
		t.Fatal("missing publish --config flag")
	}
	if got := flag.Value.Type(); got != "string" {
		t.Fatalf("publish --config flag type = %q, want string", got)
	}
}
