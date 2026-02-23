// SPDX-License-Identifier: MPL-2.0

package indexing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/registry"
)

func TestDocumentStore_Basic(t *testing.T) {
	store := NewDocumentStore()
	id := registry.NewID("app", "doc")

	_, ok := store.Get(id)
	assert.False(t, ok)

	store.Set(id, "hello", 1)
	doc, ok := store.Get(id)
	assert.True(t, ok)
	assert.Equal(t, "hello", doc.Text)
	assert.Equal(t, 1, doc.Version)

	store.Delete(id)
	_, ok = store.Get(id)
	assert.False(t, ok)
}

func TestDocumentStore_Reset(t *testing.T) {
	store := NewDocumentStore()
	id := registry.NewID("app", "doc")

	store.Set(id, "hello", 1)
	store.Reset()

	_, ok := store.Get(id)
	assert.False(t, ok)

	store.Set(id, "world", 2)
	doc, ok := store.Get(id)
	assert.True(t, ok)
	assert.Equal(t, "world", doc.Text)
}

func TestDocumentStore_IgnoresOldVersion(t *testing.T) {
	store := NewDocumentStore()
	id := registry.NewID("app", "doc")

	store.Set(id, "v2", 2)
	store.Set(id, "v1", 1)

	doc, ok := store.Get(id)
	assert.True(t, ok)
	assert.Equal(t, "v2", doc.Text)
	assert.Equal(t, 2, doc.Version)
}
