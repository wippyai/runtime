// SPDX-License-Identifier: MPL-2.0

package indexing

import (
	"sync"

	"github.com/wippyai/runtime/api/registry"
)

// Document holds in-memory text for an open editor buffer.
type Document struct {
	Text    string
	Version int
}

// DocumentStore keeps unsaved document contents for LSP indexing.
type DocumentStore struct {
	docs map[registry.ID]Document
	mu   sync.RWMutex
}

// NewDocumentStore creates a new document store.
func NewDocumentStore() *DocumentStore {
	return &DocumentStore{
		docs: make(map[registry.ID]Document),
	}
}

// Get returns the document for the given ID.
func (s *DocumentStore) Get(id registry.ID) (Document, bool) {
	if s == nil {
		return Document{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.docs == nil {
		return Document{}, false
	}
	doc, ok := s.docs[id]
	return doc, ok
}

// Set stores or updates a document.
// Updates with an older version than the stored one are ignored.
func (s *DocumentStore) Set(id registry.ID, text string, version int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.docs == nil {
		s.docs = make(map[registry.ID]Document)
	}
	if existing, ok := s.docs[id]; ok && version < existing.Version {
		return
	}
	s.docs[id] = Document{Text: text, Version: version}
}

// Delete removes a document from the store.
func (s *DocumentStore) Delete(id registry.ID) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.docs == nil {
		return
	}
	delete(s.docs, id)
}

// Reset clears all documents and releases memory.
func (s *DocumentStore) Reset() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.docs = nil
}
