// SPDX-License-Identifier: MPL-2.0

// Package standard composes the artifact formats shipped with Wippy.
package standard

import (
	"fmt"

	"github.com/wippyai/runtime/boot/deps/artifact"
	"github.com/wippyai/runtime/boot/deps/artifact/nodepackage"
)

func NewRegistry() (*artifact.Registry, error) {
	registry := artifact.NewRegistry()
	if err := registry.Register(nodepackage.New()); err != nil {
		return nil, fmt.Errorf("register built-in artifact format: %w", err)
	}
	return registry, nil
}
