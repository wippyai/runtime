// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/spf13/cobra"
	"github.com/wippyai/wapp"
)

func TestArtifactsMaterializeFromWapp(t *testing.T) {
	packPath := filepath.Join(t.TempDir(), "ui-kit.wapp")
	pack, err := os.Create(packPath)
	if err != nil {
		t.Fatal(err)
	}
	resourceID := wapp.NewID("example.ui_kit", "package")
	err = wapp.NewWriter().PackWithResources(
		wapp.Metadata{"name": "ui-kit", "version": "0.1.6"},
		nil,
		[]wapp.ResourceSpec{{
			ID: resourceID,
			Meta: wapp.Metadata{
				"artifact": map[string]any{"format": "node-package"},
			},
			FS: fstest.MapFS{
				"package.json": &fstest.MapFile{Data: []byte(
					`{"name":"@example/ui-kit","version":"0.1.6"}`,
				)},
				"dist/index.js": &fstest.MapFile{Data: []byte("export {}")},
			},
		}},
		pack,
	)
	if err != nil {
		_ = pack.Close()
		t.Fatal(err)
	}
	if err := pack.Close(); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	command := &cobra.Command{}
	command.Flags().String("root", root, "")
	if err := runArtifactsMaterialize(
		command,
		[]string{packPath, resourceID.String()},
	); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "npm", "@example", "ui-kit", "dist", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte("export {}")) {
		t.Fatalf("content = %q", data)
	}
}

func TestParseArtifactResourceIDRequiresFullID(t *testing.T) {
	for _, invalid := range []string{"", "package", ":package", "ns:", "ns:one:two"} {
		if _, err := parseArtifactResourceID(invalid); err == nil {
			t.Fatalf("parse %q succeeded", invalid)
		}
	}
}
