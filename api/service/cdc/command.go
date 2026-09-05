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
	After       map[string]any `json:"after,omitempty"`
	Before      map[string]any `json:"before,omitempty"`
	SourceID    registry.ID    `json:"source_id,omitempty"`
	Table       string         `json:"table"`
	Source      string         `json:"source"`
	Op          string         `json:"op"`
	Schema      string         `json:"schema"`
	Relation    string         `json:"relation"`
	LSN         string         `json:"lsn"`
	CommitLSN   string         `json:"commit_lsn,omitempty"`
	Cursor      string         `json:"cursor,omitempty"`
	Generation  string         `json:"generation,omitempty"`
	Transaction string         `json:"transaction,omitempty"`
	Error       string         `json:"error,omitempty"`
	Unchanged   []string       `json:"unchanged,omitempty"`
	XID         uint32         `json:"xid,omitempty"`
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
