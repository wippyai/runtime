// SPDX-License-Identifier: MPL-2.0

package cdc

import "context"

type SourceInfo struct {
	Name        string   `json:"name"`
	Slot        string   `json:"slot"`
	EventSystem string   `json:"event_system"`
	Publication string   `json:"publication,omitempty"`
	Tables      []string `json:"tables,omitempty"`
	Streaming   bool     `json:"streaming,omitempty"`
	Failover    bool     `json:"failover,omitempty"`
	Temporary   bool     `json:"temporary,omitempty"`
	Snapshot    bool     `json:"snapshot,omitempty"`
}

type SourceInspector interface {
	List() []SourceInfo
	Get(name string) (SourceInfo, bool)
}

type sourceInspectorKey struct{}

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
