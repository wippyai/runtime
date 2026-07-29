// SPDX-License-Identifier: MPL-2.0

package nodepackage

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/wippyai/runtime/cmd/internal/artifact"
	"github.com/wippyai/wapp"
)

func TestInspect(t *testing.T) {
	input := artifact.InspectInput{
		Filesystem: fstest.MapFS{
			"package.json": &fstest.MapFile{Data: []byte(
				`{"name":"@example/ui-kit","version":"0.1.6","scripts":{"build":"vite build"}}`,
			)},
			"dist/index.js": &fstest.MapFile{Data: []byte("export {}")},
		},
		ModuleVersion: "0.1.6",
		ResourceID:    wapp.NewID("example.ui_kit", "package"),
	}
	got, err := New().Inspect(context.Background(), input)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if got.Identity != "@example/ui-kit" || got.Version != "0.1.6" {
		t.Fatalf("descriptor = %+v", got)
	}
	if got.RelativePath != "npm/@example/ui-kit" {
		t.Fatalf("relative path = %q", got.RelativePath)
	}
}

func TestInspectRejectsInvalidPackages(t *testing.T) {
	tests := map[string]struct {
		manifest      string
		moduleVersion string
		want          string
	}{
		"missing name": {
			manifest: `{"version":"1.0.0"}`,
			want:     "name is required",
		},
		"invalid name": {
			manifest: `{"name":"@scope/../escape","version":"1.0.0"}`,
			want:     "invalid scoped package name",
		},
		"bad version": {
			manifest: `{"name":"@scope/pkg","version":"latest"}`,
			want:     "is not semantic",
		},
		"module mismatch": {
			manifest:      `{"name":"@scope/pkg","version":"1.0.0"}`,
			moduleVersion: "2.0.0",
			want:          "does not match module version",
		},
		"lifecycle script": {
			manifest: `{"name":"@scope/pkg","version":"1.0.0","scripts":{"postinstall":"node setup.js"}}`,
			want:     `lifecycle script "postinstall" is not allowed`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := New().Inspect(context.Background(), artifact.InspectInput{
				Filesystem: fstest.MapFS{
					"package.json": &fstest.MapFile{Data: []byte(test.manifest)},
				},
				ModuleVersion: test.moduleVersion,
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}
