// Package storeapi provides store command types for the dispatcher system.
package storeapi

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/store"
)

func init() {
	dispatcher.MustRegisterCommands("store",
		CmdStoreGet, CmdStoreSet, CmdStoreDelete, CmdStoreHas,
	)
}

// Command IDs for store operations.
// Range 120-129 is reserved for key-value store commands.
const (
	CmdStoreGet    dispatcher.CommandID = 120 // Get value by key
	CmdStoreSet    dispatcher.CommandID = 121 // Set value with key
	CmdStoreDelete dispatcher.CommandID = 122 // Delete key
	CmdStoreHas    dispatcher.CommandID = 123 // Check if key exists
)

// StoreGetCmd retrieves a value from the store.
type StoreGetCmd struct {
	Store store.Store
	Key   registry.ID
}

var storeGetCmdPool = sync.Pool{New: func() any { return &StoreGetCmd{} }}

func AcquireStoreGetCmd() *StoreGetCmd             { return storeGetCmdPool.Get().(*StoreGetCmd) }
func (c *StoreGetCmd) CmdID() dispatcher.CommandID { return CmdStoreGet }
func (c *StoreGetCmd) Release() {
	c.Store = nil
	c.Key = registry.ID{}
	storeGetCmdPool.Put(c)
}

// StoreSetCmd sets a value in the store.
type StoreSetCmd struct {
	Store store.Store
	Entry store.Entry
}

var storeSetCmdPool = sync.Pool{New: func() any { return &StoreSetCmd{} }}

func AcquireStoreSetCmd() *StoreSetCmd             { return storeSetCmdPool.Get().(*StoreSetCmd) }
func (c *StoreSetCmd) CmdID() dispatcher.CommandID { return CmdStoreSet }
func (c *StoreSetCmd) Release() {
	c.Store = nil
	c.Entry = store.Entry{}
	storeSetCmdPool.Put(c)
}

// StoreDeleteCmd deletes a key from the store.
type StoreDeleteCmd struct {
	Store store.Store
	Key   registry.ID
}

var storeDeleteCmdPool = sync.Pool{New: func() any { return &StoreDeleteCmd{} }}

func AcquireStoreDeleteCmd() *StoreDeleteCmd          { return storeDeleteCmdPool.Get().(*StoreDeleteCmd) }
func (c *StoreDeleteCmd) CmdID() dispatcher.CommandID { return CmdStoreDelete }
func (c *StoreDeleteCmd) Release() {
	c.Store = nil
	c.Key = registry.ID{}
	storeDeleteCmdPool.Put(c)
}

// StoreHasCmd checks if a key exists.
type StoreHasCmd struct {
	Store store.Store
	Key   registry.ID
}

var storeHasCmdPool = sync.Pool{New: func() any { return &StoreHasCmd{} }}

func AcquireStoreHasCmd() *StoreHasCmd             { return storeHasCmdPool.Get().(*StoreHasCmd) }
func (c *StoreHasCmd) CmdID() dispatcher.CommandID { return CmdStoreHas }
func (c *StoreHasCmd) Release() {
	c.Store = nil
	c.Key = registry.ID{}
	storeHasCmdPool.Put(c)
}

// StoreGetResponse contains the result of a get operation.
type StoreGetResponse struct {
	Value payload.Payload
	Error error
}

// StoreSetResponse contains the result of a set operation.
type StoreSetResponse struct {
	Error error
}

// StoreDeleteResponse contains the result of a delete operation.
type StoreDeleteResponse struct {
	Error error
}

// StoreHasResponse contains the result of a has operation.
type StoreHasResponse struct {
	Exists bool
	Error  error
}
