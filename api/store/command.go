// SPDX-License-Identifier: MPL-2.0

package store

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

func init() {
	dispatcher.MustRegisterCommands("store",
		Get, Set, Delete, Has, EntryCommand, ListCommand, PutCommand,
	)
}

// Command IDs for store operations.
// Range 120-129 is reserved for key-value store commands.
const (
	Get          dispatcher.CommandID = 120 // Get value by key
	Set          dispatcher.CommandID = 121 // Set value with key
	Delete       dispatcher.CommandID = 122 // Delete key
	Has          dispatcher.CommandID = 123 // Check if key exists
	EntryCommand dispatcher.CommandID = 124 // Get value with metadata
	ListCommand  dispatcher.CommandID = 125 // List keys deterministically
	PutCommand   dispatcher.CommandID = 126 // Rich put with conditions
)

// GetCmd retrieves a value from the store.
type GetCmd struct {
	Store Store
	Key   registry.ID
}

var getCmdPool = sync.Pool{New: func() any { return &GetCmd{} }}

// AcquireGetCmd returns a pooled GetCmd.
func AcquireGetCmd() *GetCmd                  { return getCmdPool.Get().(*GetCmd) }
func (c *GetCmd) CmdID() dispatcher.CommandID { return Get }
func (c *GetCmd) Release() {
	c.Store = nil
	c.Key = registry.ID{}
	getCmdPool.Put(c)
}

// SetCmd sets a value in the store.
type SetCmd struct {
	Store Store
	Entry Entry
}

var setCmdPool = sync.Pool{New: func() any { return &SetCmd{} }}

// AcquireSetCmd returns a pooled SetCmd.
func AcquireSetCmd() *SetCmd                  { return setCmdPool.Get().(*SetCmd) }
func (c *SetCmd) CmdID() dispatcher.CommandID { return Set }
func (c *SetCmd) Release() {
	c.Store = nil
	c.Entry = Entry{}
	setCmdPool.Put(c)
}

// DeleteCmd deletes a key from the store.
type DeleteCmd struct {
	Store Store
	Key   registry.ID
}

var deleteCmdPool = sync.Pool{New: func() any { return &DeleteCmd{} }}

// AcquireDeleteCmd returns a pooled DeleteCmd.
func AcquireDeleteCmd() *DeleteCmd               { return deleteCmdPool.Get().(*DeleteCmd) }
func (c *DeleteCmd) CmdID() dispatcher.CommandID { return Delete }
func (c *DeleteCmd) Release() {
	c.Store = nil
	c.Key = registry.ID{}
	deleteCmdPool.Put(c)
}

// HasCmd checks if a key exists.
type HasCmd struct {
	Store Store
	Key   registry.ID
}

var hasCmdPool = sync.Pool{New: func() any { return &HasCmd{} }}

// AcquireHasCmd returns a pooled HasCmd.
func AcquireHasCmd() *HasCmd                  { return hasCmdPool.Get().(*HasCmd) }
func (c *HasCmd) CmdID() dispatcher.CommandID { return Has }
func (c *HasCmd) Release() {
	c.Store = nil
	c.Key = registry.ID{}
	hasCmdPool.Put(c)
}

// EntryCmd retrieves a value with store metadata.
type EntryCmd struct {
	Store Store
	Key   registry.ID
}

var entryCmdPool = sync.Pool{New: func() any { return &EntryCmd{} }}

// AcquireEntryCmd returns a pooled EntryCmd.
func AcquireEntryCmd() *EntryCmd                { return entryCmdPool.Get().(*EntryCmd) }
func (c *EntryCmd) CmdID() dispatcher.CommandID { return EntryCommand }
func (c *EntryCmd) Release() {
	c.Store = nil
	c.Key = registry.ID{}
	entryCmdPool.Put(c)
}

// ListCmd lists store entries.
type ListCmd struct {
	Store Store
	Opts  ListOptions
}

var listCmdPool = sync.Pool{New: func() any { return &ListCmd{} }}

// AcquireListCmd returns a pooled ListCmd.
func AcquireListCmd() *ListCmd                 { return listCmdPool.Get().(*ListCmd) }
func (c *ListCmd) CmdID() dispatcher.CommandID { return ListCommand }
func (c *ListCmd) Release() {
	c.Store = nil
	c.Opts = ListOptions{}
	listCmdPool.Put(c)
}

// PutCmd writes a value with optional preconditions.
type PutCmd struct {
	Store Store
	Value payload.Payload
	Key   registry.ID
	Opts  PutOptions
}

var putCmdPool = sync.Pool{New: func() any { return &PutCmd{} }}

// AcquirePutCmd returns a pooled PutCmd.
func AcquirePutCmd() *PutCmd                  { return putCmdPool.Get().(*PutCmd) }
func (c *PutCmd) CmdID() dispatcher.CommandID { return PutCommand }
func (c *PutCmd) Release() {
	c.Store = nil
	c.Key = registry.ID{}
	c.Value = nil
	c.Opts = PutOptions{}
	putCmdPool.Put(c)
}

// GetResponse contains the result of a get operation.
type GetResponse struct {
	Value payload.Payload
	Error error
}

// SetResponse contains the result of a set operation.
type SetResponse struct {
	Error error
}

// DeleteResponse contains the result of a delete operation.
type DeleteResponse struct {
	Error    error
	NotFound bool
}

// HasResponse contains the result of a has operation.
type HasResponse struct {
	Error  error
	Exists bool
}

// EntryResponse contains an entry plus metadata.
type EntryResponse struct {
	Error error
	Entry VersionedEntry
}

// ListResponse contains a deterministic list page.
type ListResponse struct {
	Error error
	Page  Page
}

// PutResponse contains the stored entry.
type PutResponse struct {
	Error error
	Entry VersionedEntry
}
