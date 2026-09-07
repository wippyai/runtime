// SPDX-License-Identifier: MPL-2.0

package component

import (
	"github.com/wippyai/runtime/api/registry"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"go.uber.org/zap"
)

type optionDeprecations interface {
	DeprecatedOptionPaths() []wasmapi.DeprecatedOptionPath
}

// LogOptionDeprecations reports accepted legacy paths after an entry is added
// or updated successfully. It is never called from guest execution. Diagnostic
// sources expose paths only, so configuration values cannot enter these logs.
func LogOptionDeprecations(log *zap.Logger, id registry.ID, source optionDeprecations) {
	if log == nil || source == nil {
		return
	}
	for _, warning := range source.DeprecatedOptionPaths() {
		log.Warn("deprecated WASM entry option path",
			zap.String("code", "entry.options.deprecated_path"),
			zap.String("entry", id.String()),
			zap.String("path", warning.Path),
			zap.String("replacement", warning.Replacement))
	}
}
