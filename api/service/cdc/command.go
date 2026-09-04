// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
)

func init() {
	dispatcher.MustRegisterCommands("cdc", Subscribe)
}

const (
	Subscribe dispatcher.CommandID = 172
)

type StreamOptions struct {
	// After is an opaque source cursor. A driver that cannot resume from a
	// cursor must return ErrUnsupported rather than silently ignore it.
	After  string
	Tables []string
	Ops    []string
	// MaxBytes bounds the retained logical size of one subscriber backlog.
	// Zero selects DefaultMaxStreamBytes; negative values are invalid.
	MaxBytes int64
	Buffer   int
	Snapshot bool
}

type Change struct {
	// Unchanged lists after-image columns omitted because the database did
	// not resend their values. Absence is distinct from SQL NULL and text.
	Unchanged []string       `json:"unchanged,omitempty"`
	Before    map[string]any `json:"before,omitempty"`
	After     map[string]any `json:"after,omitempty"`
	Source    string         `json:"source"`
	// SourceID is the canonical registry identity. Source is retained as the
	// legacy wire representation used by existing Lua consumers.
	SourceID    registry.ID `json:"source_id,omitempty"`
	Op          string      `json:"op"`
	Schema      string      `json:"schema"`
	Table       string      `json:"table"`
	Relation    string      `json:"relation"`
	LSN         string      `json:"lsn"`
	CommitLSN   string      `json:"commit_lsn,omitempty"`
	Cursor      string      `json:"cursor,omitempty"`
	Generation  string      `json:"generation,omitempty"`
	Transaction string      `json:"transaction,omitempty"`
	Error       string      `json:"error,omitempty"`
	XID         uint32      `json:"xid,omitempty"`
}

type SubscribeCmd struct {
	PID     pid.PID
	Source  string
	Topic   string
	Options StreamOptions
}

func (c SubscribeCmd) CmdID() dispatcher.CommandID {
	return Subscribe
}

type Subscription struct {
	Stop   func()
	Topic  string
	Source string
}

type ChangeStream interface {
	Changes() <-chan Change
	Close()
}

type SourceStreamer interface {
	Stream(ctx context.Context, source string, opts StreamOptions) (ChangeStream, SourceInfo, error)
}
