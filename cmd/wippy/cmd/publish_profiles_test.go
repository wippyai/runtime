// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/boot/deps/config"
)

func TestAddPublishedRuntimeProfileMetadataExportsAllAppProfiles(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
vars:
  db_host: localhost
profiles:
  local:
    override:
      "app:db:kind": db.sql.sqlite
  prod:
    vars:
      db_host: db.internal
    override:
      "app:db:kind": db.sql.postgres
    disable:
      namespaces:
        add:
          - app.dev.**
`)

	metadata := attrs.Bag{
		"runtime": map[string]any{
			"logger": map[string]any{
				"level": "info",
			},
		},
	}

	require.NoError(t, addPublishedRuntimeProfileMetadata(metadata, dir, config.PublishProfilesConfig{}))

	runtime := requireMap(t, metadata["runtime"])
	require.Equal(t, map[string]any{"level": "info"}, requireMap(t, runtime["logger"]))

	vars := requireMap(t, runtime["vars"])
	require.Equal(t, "localhost", vars["db_host"])

	profiles := requireMap(t, runtime["profiles"])
	local := requireMap(t, profiles["local"])
	localOverride := requireMap(t, local["override"])
	require.Equal(t, "db.sql.sqlite", localOverride["app:db:kind"])

	prod := requireMap(t, profiles["prod"])
	prodVars := requireMap(t, prod["vars"])
	require.Equal(t, "db.internal", prodVars["db_host"])
	prodOverride := requireMap(t, prod["override"])
	require.Equal(t, "db.sql.postgres", prodOverride["app:db:kind"])
	prodDisable := requireMap(t, prod["disable"])
	require.Equal(t, []any{"app.dev.**"}, prodDisable["namespaces.add"])
}

func TestAddPublishedRuntimeProfileMetadataUsesConfiguredSource(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, "profiles/public.yaml", `version: "1.0"
profiles:
  public:
    override:
      "app:http:addr": ":8080"
`)

	metadata := attrs.Bag{}
	require.NoError(t, addPublishedRuntimeProfileMetadata(metadata, dir, config.PublishProfilesConfig{Source: "profiles/public.yaml"}))

	runtime := requireMap(t, metadata["runtime"])
	profiles := requireMap(t, runtime["profiles"])
	require.Contains(t, profiles, "public")
}

func TestAddPublishedRuntimeProfileMetadataIncludesOnlySelectedProfiles(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
profiles:
  local:
    logger:
      level: debug
  production:
    logger:
      level: info
`)

	metadata := attrs.Bag{}
	require.NoError(t, addPublishedRuntimeProfileMetadata(metadata, dir, config.PublishProfilesConfig{
		Include: []string{"production"},
	}))

	profiles := requireMap(t, requireMap(t, metadata["runtime"])["profiles"])
	require.NotContains(t, profiles, "local")
	require.Contains(t, profiles, "production")
}

func TestPublishedRuntimeProfilesIgnoreRuntimeConfigSelection(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
profiles:
  production:
    logger:
      level: info
`)
	overlayDir := t.TempDir()
	writeRuntimeProfileConfig(t, overlayDir, `developer.yaml`, `version: "1.0"
profiles:
  production:
    logger:
      level: debug
  workspace:
    workspace:
      replacements:
        acme/http: ../http
`)
	setTestConfigFiles(t, filepath.Join(dir, ".wippy.yaml"), filepath.Join(overlayDir, "developer.yaml"))

	metadata := attrs.Bag{}
	require.NoError(t, addPublishedRuntimeProfileMetadata(metadata, dir, config.PublishProfilesConfig{}))

	profiles := requireMap(t, requireMap(t, metadata["runtime"])["profiles"])
	require.NotContains(t, profiles, "workspace")
	production := requireMap(t, profiles["production"])
	require.Equal(t, "info", requireMap(t, production["logger"])["level"])
}

func TestAddPublishedRuntimeProfileMetadataEmptyIncludePublishesNoProfiles(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
profiles:
  local:
    logger:
      level: debug
`)

	metadata := attrs.Bag{}
	require.NoError(t, addPublishedRuntimeProfileMetadata(metadata, dir, config.PublishProfilesConfig{
		Include: []string{},
	}))
	require.NotContains(t, metadata, "runtime")
}

func TestAddPublishedRuntimeProfileMetadataNeverExportsWorkspace(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
workspace:
  replacements:
    acme/http: ../http
profiles:
  local:
    workspace:
      replacements:
        acme/http: ../local-http
    logger:
      level: debug
`)

	metadata := attrs.Bag{}
	require.NoError(t, addPublishedRuntimeProfileMetadata(metadata, dir, config.PublishProfilesConfig{}))

	runtime := requireMap(t, metadata["runtime"])
	require.NotContains(t, runtime, "workspace")
	local := requireMap(t, requireMap(t, runtime["profiles"])["local"])
	require.NotContains(t, local, "workspace")
	require.Equal(t, "debug", requireMap(t, local["logger"])["level"])
}

func TestAddPublishedRuntimeProfileMetadataRejectsUnknownIncludedProfile(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
profiles:
  production:
    logger:
      level: info
`)

	err := addPublishedRuntimeProfileMetadata(attrs.Bag{}, dir, config.PublishProfilesConfig{
		Include: []string{"prodution"},
	})
	require.ErrorContains(t, err, `publish profile "prodution" not found`)
}

func TestAddPublishedRuntimeProfileMetadataDisabled(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
profiles:
  local:
    override:
      "app:db:kind": db.sql.sqlite
`)

	enabled := false
	metadata := attrs.Bag{}
	require.NoError(t, addPublishedRuntimeProfileMetadata(metadata, dir, config.PublishProfilesConfig{Enabled: &enabled}))
	require.NotContains(t, metadata, "runtime")
}

func TestAddPublishedRuntimeProfileMetadataDisabledErrorsOnMetadataProfiles(t *testing.T) {
	dir := t.TempDir()

	enabled := false
	metadata := attrs.Bag{
		"runtime": map[string]any{
			"profiles": map[string]any{
				"local": map[string]any{},
			},
		},
	}

	err := addPublishedRuntimeProfileMetadata(metadata, dir, config.PublishProfilesConfig{Enabled: &enabled})
	require.Error(t, err)
	require.Contains(t, err.Error(), "metadata.runtime.profiles is not supported")
}

func TestAddPublishedRuntimeProfileMetadataErrorsOnMetadataProfiles(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
logger:
  level: debug
`)

	metadata := attrs.Bag{
		"runtime": map[string]any{
			"profiles": map[string]any{
				"public": map[string]any{
					"override": map[string]any{
						"app:db:kind": "db.sql.postgres",
					},
				},
			},
		},
	}

	err := addPublishedRuntimeProfileMetadata(metadata, dir, config.PublishProfilesConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "metadata.runtime.profiles is not supported")
}

func TestAddPublishedRuntimeProfileMetadataErrorsOnDottedMetadataProfiles(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
profiles:
  local:
    override:
      "app:db:kind": db.sql.sqlite
`)

	metadata := attrs.Bag{
		"runtime.profiles.prod.override.app:db:kind": "db.sql.postgres",
	}

	err := addPublishedRuntimeProfileMetadata(metadata, dir, config.PublishProfilesConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "metadata.runtime.profiles is not supported")
}

func TestAddPublishedRuntimeProfileMetadataErrorsOnMetadataVarsWithSourceProfiles(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
vars:
  port: 8080
profiles:
  local:
    override:
      "app:http:addr": ":${port}"
`)

	metadata := attrs.Bag{
		"runtime": map[string]any{
			"vars": map[string]any{"port": 9000},
		},
	}

	err := addPublishedRuntimeProfileMetadata(metadata, dir, config.PublishProfilesConfig{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "runtime vars are defined in both")
}

func TestAddPublishedRuntimeProfileMetadataNoProfilesLeavesMetadata(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
logger:
  level: debug
`)

	metadata := attrs.Bag{"name": "app"}
	require.NoError(t, addPublishedRuntimeProfileMetadata(metadata, dir, config.PublishProfilesConfig{}))
	require.Equal(t, attrs.Bag{"name": "app"}, metadata)
}

func writeRuntimeProfileConfig(t *testing.T, dir, rel, body string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func requireMap(t *testing.T, value any) map[string]any {
	t.Helper()
	typed, ok := value.(map[string]any)
	require.Truef(t, ok, "value has type %T", value)
	return typed
}
