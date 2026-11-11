// SPDX-License-Identifier: MPL-2.0

// Package xml provides command types for streaming XML readers and writers.
package xml

import (
	"github.com/wippyai/runtime/api/dispatcher"
)

func init() {
	dispatcher.MustRegisterCommands("xml",
		ReaderFill, ReaderNode, ReaderSkip, ReaderCopy,
		RecordsRead, WriterFlush,
	)
}

// Command IDs for XML operations that read or write a stream.
// Range 220-229 is reserved for XML commands.
const (
	ReaderFill  dispatcher.CommandID = 220 // Read ahead the next events of a reader
	ReaderNode  dispatcher.CommandID = 221 // Finish materializing the current element
	ReaderSkip  dispatcher.CommandID = 222 // Finish skipping the current element
	ReaderCopy  dispatcher.CommandID = 223 // Copy the current element into a writer
	RecordsRead dispatcher.CommandID = 224 // Read the next batch of selected records
	WriterFlush dispatcher.CommandID = 225 // Write buffered output to the destination
)

// FillCmd reads ahead the next events of a reader.
type FillCmd struct {
	Reader uint64
}

// CmdID implements dispatcher.Command.
func (c FillCmd) CmdID() dispatcher.CommandID { return ReaderFill }

// NodeCmd materializes the current element of a reader into its node batch.
type NodeCmd struct {
	Reader uint64
}

// CmdID implements dispatcher.Command.
func (c NodeCmd) CmdID() dispatcher.CommandID { return ReaderNode }

// SkipCmd discards the current element of a reader.
type SkipCmd struct {
	Reader uint64
}

// CmdID implements dispatcher.Command.
func (c SkipCmd) CmdID() dispatcher.CommandID { return ReaderSkip }

// CopyCmd copies the current element of a reader into a writer, flushing the
// writer to its destination as its buffer fills.
type CopyCmd struct {
	Reader uint64
	Writer uint64
}

// CmdID implements dispatcher.Command.
func (c CopyCmd) CmdID() dispatcher.CommandID { return ReaderCopy }

// RecordsReadCmd reads up to Count selected records into the reader's record batch.
type RecordsReadCmd struct {
	Reader uint64
	Count  int
}

// CmdID implements dispatcher.Command.
func (c RecordsReadCmd) CmdID() dispatcher.CommandID { return RecordsRead }

// FlushCmd writes buffered writer output to its destination. Sync also flushes
// the destination when it supports flushing.
type FlushCmd struct {
	Writer uint64
	Sync   bool
}

// CmdID implements dispatcher.Command.
func (c FlushCmd) CmdID() dispatcher.CommandID { return WriterFlush }
