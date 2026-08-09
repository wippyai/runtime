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
      "app:db:host": "${db_host}"
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

	require.NotContains(t, runtime, "vars", "profile-local variables must not cause unrelated base variables to be published")

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

func TestAddPublishedRuntimeMetadataExportsSelectedBaseConfigAndProfiles(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
security:
  strict_mode: true
registry:
  enable_history: true
  dispatch_internal_kinds:
    - registry.entry
    - ns.requirement
vars:
  port: 8085
  signing_key: must-never-be-packed
override:
  "app:gateway:addr": ":${port}"
profiles:
  local:
    override:
      "app:gateway:addr": ":18085"
workspace:
  replacements:
    acme/http: ../http
secrets:
  signing_key: must-never-be-packed
`)

	metadata := attrs.Bag{}
	disabled := false
	require.NoError(t, addPublishedRuntimeMetadata(metadata, dir, config.PublishConfig{
		Runtime: config.PublishRuntimeConfig{
			Sections: []string{"security", "registry", "override"},
		},
		Profiles: config.PublishProfilesConfig{Enabled: &disabled},
	}))

	runtime := requireMap(t, metadata["runtime"])
	require.Equal(t, true, requireMap(t, runtime["security"])["strict_mode"])
	registry := requireMap(t, runtime["registry"])
	require.Equal(t, true, registry["enable_history"])
	require.Equal(t, []any{"registry.entry", "ns.requirement"}, registry["dispatch_internal_kinds"])
	require.Equal(t, ":${port}", requireMap(t, runtime["override"])["app:gateway:addr"])
	require.Equal(t, 8085, requireMap(t, runtime["vars"])["port"])
	require.NotContains(t, requireMap(t, runtime["vars"]), "signing_key")
	require.NotContains(t, runtime, "profiles")
	require.NotContains(t, runtime, "workspace")
	require.NotContains(t, runtime, "secrets")
}

func TestAddPublishedRuntimeMetadataRejectsMachineLocalSections(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
workspace:
  replacements:
    acme/http: ../http
`)

	err := addPublishedRuntimeMetadata(attrs.Bag{}, dir, config.PublishConfig{
		Runtime: config.PublishRuntimeConfig{Sections: []string{"workspace"}},
	})
	require.ErrorContains(t, err, `runtime section "workspace" is machine-local`)
}

func TestAddPublishedRuntimeMetadataNeverResolvesPublisherSecrets(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WIPPY_PUBLISH_TEST_SECRET", "publisher-secret-value")
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
vars:
  history_dsn: ${env:WIPPY_PUBLISH_TEST_SECRET}
registry:
  history_dsn: ${history_dsn}
`)

	metadata := attrs.Bag{}
	err := addPublishedRuntimeMetadata(metadata, dir, config.PublishConfig{
		Runtime: config.PublishRuntimeConfig{Sections: []string{"registry"}},
	})
	require.ErrorContains(t, err, "vars.history_dsn references the publisher environment")
	require.NotContains(t, err.Error(), "publisher-secret-value")
	require.NotContains(t, metadata, "runtime")
}

func TestAddPublishedRuntimeMetadataRejectsUndefinedReferencedVariable(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
override:
  "app:gateway:addr": ":${missing_port}"
`)

	err := addPublishedRuntimeMetadata(attrs.Bag{}, dir, config.PublishConfig{
		Runtime: config.PublishRuntimeConfig{Sections: []string{"override"}},
	})
	require.ErrorContains(t, err, `published runtime sections references undefined runtime variable "missing_port"`)
}

func TestAddPublishedRuntimeProfileMetadataExportsOnlyReferencedBaseVariables(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
vars:
  public_host: localhost
  public_timeout: 30s
profiles:
  local:
    override:
      "app:db:host": "${public_host}"
`)

	metadata := attrs.Bag{}
	require.NoError(t, addPublishedRuntimeProfileMetadata(metadata, dir, config.PublishProfilesConfig{}))
	vars := requireMap(t, requireMap(t, metadata["runtime"])["vars"])
	require.Equal(t, "localhost", vars["public_host"])
	require.NotContains(t, vars, "public_timeout")
}

func TestAddPublishedRuntimeMetadataProfileVariablesSelectBaseDependenciesTransitively(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
vars:
  public_host: localhost
  public_url: "http://${public_host}:8085"
  signing_key: must-never-be-packed
profiles:
  local:
    vars:
      api_url: "${public_url}/api"
    override:
      "app.env:defaults:values.PUBLIC_API_URL": "${api_url}"
`)

	metadata := attrs.Bag{}
	require.NoError(t, addPublishedRuntimeProfileMetadata(metadata, dir, config.PublishProfilesConfig{}))
	runtime := requireMap(t, metadata["runtime"])
	vars := requireMap(t, runtime["vars"])
	require.Equal(t, "localhost", vars["public_host"])
	require.Equal(t, "http://${public_host}:8085", vars["public_url"])
	require.NotContains(t, vars, "signing_key")

	local := requireMap(t, requireMap(t, runtime["profiles"])["local"])
	require.Equal(t, "${public_url}/api", requireMap(t, local["vars"])["api_url"])
}

func TestAddPublishedRuntimeMetadataExportsExplicitPublicVariables(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
vars:
  public_host: localhost
  public_url: "http://${public_host}:8085"
  signing_key: must-never-be-packed
`)

	metadata := attrs.Bag{}
	require.NoError(t, addPublishedRuntimeMetadata(metadata, dir, config.PublishConfig{
		Runtime: config.PublishRuntimeConfig{Vars: []string{"public_url"}},
	}))
	vars := requireMap(t, requireMap(t, metadata["runtime"])["vars"])
	require.Equal(t, "localhost", vars["public_host"])
	require.Equal(t, "http://${public_host}:8085", vars["public_url"])
	require.NotContains(t, vars, "signing_key")
}

func TestAddPublishedRuntimeMetadataRejectsUnknownExplicitVariable(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
vars:
  public_url: http://localhost:8085
`)

	err := addPublishedRuntimeMetadata(attrs.Bag{}, dir, config.PublishConfig{
		Runtime: config.PublishRuntimeConfig{Vars: []string{"missing"}},
	})
	require.ErrorContains(t, err, `publish runtime variable "missing" not found`)
}

func TestAddPublishedRuntimeMetadataRequiresExplicitAllowList(t *testing.T) {
	err := addPublishedRuntimeMetadata(attrs.Bag{}, t.TempDir(), config.PublishConfig{
		Runtime: config.PublishRuntimeConfig{Source: "config/runtime.yaml"},
	})
	require.ErrorContains(t, err, "requires an explicit sections or vars allow-list")
}

func TestAddPublishedRuntimeMetadataRejectsAmbiguousSectionMetadata(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeProfileConfig(t, dir, `.wippy.yaml`, `version: "1.0"
security:
  strict_mode: true
`)
	metadata := attrs.Bag{"runtime": map[string]any{
		"security": map[string]any{"strict_mode": false},
	}}

	err := addPublishedRuntimeMetadata(metadata, dir, config.PublishConfig{
		Runtime: config.PublishRuntimeConfig{Sections: []string{"security"}},
	})
	require.ErrorContains(t, err, `runtime section "security" is defined in both`)
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
