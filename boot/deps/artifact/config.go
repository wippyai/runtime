// SPDX-License-Identifier: MPL-2.0

package artifact

import "github.com/wippyai/runtime/api/boot"

const (
	// ConfigName is the application configuration section owned by artifacts.
	ConfigName boot.Name = "artifact"
	// ConfigMaterializationRoot overrides the application root for materialized artifacts.
	ConfigMaterializationRoot boot.Name = "materialization_root"
)

// ConfiguredRoot returns the configured materialization root or fallback.
func ConfiguredRoot(cfg boot.Config, fallback string) string {
	if cfg == nil {
		return fallback
	}
	return cfg.Sub(ConfigName).GetString(ConfigMaterializationRoot, fallback)
}
