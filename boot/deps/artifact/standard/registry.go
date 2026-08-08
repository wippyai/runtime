// SPDX-License-Identifier: MPL-2.0

// Package standard composes the artifact formats shipped with Wippy.
package standard

import (
	"fmt"

	"github.com/wippyai/runtime/boot/deps/artifact"
	"github.com/wippyai/runtime/boot/deps/artifact/nodepackage"
)

// NewRegistry composes Wippy's built-in formats followed by caller-provided
// formats. Additional formats use the same duplicate-name and root-ownership
// validation as built-ins.
func NewRegistry(additional ...artifact.Format) (*artifact.Registry, error) {
	registry := artifact.NewRegistry()
	formats := append([]artifact.Format{nodepackage.New()}, additional...)
	for _, format := range formats {
		if err := registry.Register(format); err != nil {
			return nil, fmt.Errorf("register artifact format: %w", err)
		}
	}
	return registry, nil
}
