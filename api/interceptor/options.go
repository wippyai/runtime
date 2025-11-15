// Package interceptor provides request and operation interception.
package interceptor

import "github.com/wippyai/runtime/api/attrs"

// Bag is an alias to attrs.Bag for backward compatibility
type Bag = attrs.Bag

// NewBag creates a new options bag
func NewBag() Bag {
	return attrs.NewBag()
}

// NewBagFrom creates a new options bag from existing data
func NewBagFrom(data map[string]any) Bag {
	return attrs.NewBagFrom(data)
}
