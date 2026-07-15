// SPDX-License-Identifier: MPL-2.0

package cdc

import "context"

type SourceInfo struct {
	Name        string   `json:"name"`
	Slot        string   `json:"slot"`
	Publication string   `json:"publication,omitempty"`
	Engine      string   `json:"engine,omitempty"`
	File        string   `json:"file,omitempty"`
	DBResource  string   `json:"db_resource,omitempty"`
	Epoch       string   `json:"epoch,omitempty"`
	Error       string   `json:"error,omitempty"`
	Tables      []string `json:"tables,omitempty"`
	Streaming   bool     `json:"streaming,omitempty"`
	Failover    bool     `json:"failover,omitempty"`
	Temporary   bool     `json:"temporary,omitempty"`
	Snapshot    bool     `json:"snapshot,omitempty"`
	Faulted     bool     `json:"faulted,omitempty"`
}

type SourceInspector interface {
	List() []SourceInfo
	Get(name string) (SourceInfo, bool)
}

type sourceInspectorKey struct{}
type sourceStreamerKey struct{}

func WithSourceInspector(ctx context.Context, inspector SourceInspector) context.Context {
	if inspector == nil {
		return ctx
	}
	return context.WithValue(ctx, sourceInspectorKey{}, inspector)
}

func GetSourceInspector(ctx context.Context) SourceInspector {
	v, _ := ctx.Value(sourceInspectorKey{}).(SourceInspector)
	return v
}

func WithSourceStreamer(ctx context.Context, streamer SourceStreamer) context.Context {
	if streamer == nil {
		return ctx
	}
	return context.WithValue(ctx, sourceStreamerKey{}, streamer)
}

func GetSourceStreamer(ctx context.Context) SourceStreamer {
	v, _ := ctx.Value(sourceStreamerKey{}).(SourceStreamer)
	return v
}
