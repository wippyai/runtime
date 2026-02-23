// SPDX-License-Identifier: MPL-2.0

package metrics

import "errors"

// Sentinel errors for metrics operations.
var (
	ErrClosed          = errors.New("collector is closed")
	ErrExporterExists  = errors.New("exporter already registered")
	ErrInvalidExporter = errors.New("invalid exporter")
)
