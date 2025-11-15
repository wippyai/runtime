// Package registry provides service registry and entry management.
package registry

import "github.com/wippyai/runtime/api/attrs"

// Metadata is an alias to attrs.Bag for storing arbitrary key-value metadata associated with an entry.
type Metadata = attrs.Bag

// NewMetadata creates a new Metadata instance.
func NewMetadata() Metadata {
	return attrs.NewBag()
}

// NewMetadataFrom creates a new Metadata instance from existing data.
func NewMetadataFrom(data map[string]any) Metadata {
	return attrs.NewBagFrom(data)
}
