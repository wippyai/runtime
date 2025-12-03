// Package registry provides service registry and entry management.
package registry

import "github.com/wippyai/runtime/api/attrs"

// Metadata is an alias to attrs.Bag for storing arbitrary key-value metadata associated with an entry.
//
// Deprecated: Use attrs.Bag directly instead. This alias will be removed in a future version.
type Metadata = attrs.Bag

// NewMetadata creates a new Metadata instance.
//
// Deprecated: Use attrs.NewBag() directly instead.
func NewMetadata() Metadata {
	return attrs.NewBag()
}

// NewMetadataFrom creates a new Metadata instance from existing data.
//
// Deprecated: Use attrs.NewBagFrom() directly instead.
func NewMetadataFrom(data map[string]any) Metadata {
	return attrs.NewBagFrom(data)
}
