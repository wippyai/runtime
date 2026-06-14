// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/pid"
)

func init() {
	dispatcher.MustRegisterCommands("cdc", Subscribe)
}

const (
	Subscribe dispatcher.CommandID = 170
)

type StreamOptions struct {
	Tables []string
	Ops    []string
	Buffer int
}

type Change struct {
	Before    map[string]any `json:"before,omitempty"`
	After     map[string]any `json:"after,omitempty"`
	Source    string         `json:"source"`
	Op        string         `json:"op"`
	Schema    string         `json:"schema"`
	Table     string         `json:"table"`
	Relation  string         `json:"relation"`
	LSN       string         `json:"lsn"`
	CommitLSN string         `json:"commit_lsn,omitempty"`
	XID       uint32         `json:"xid,omitempty"`
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
