// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
)

// SourceState is the driver-neutral lifecycle state exposed by a CDC source.
// Driver-specific health details remain in SourceInfo.Error and the legacy
// compatibility fields below.
type SourceState string

const (
	SourceStateUnknown  SourceState = "unknown"
	SourceStateStarting SourceState = "starting"
	SourceStateRunning  SourceState = "running"
	SourceStateFaulted  SourceState = "faulted"
	SourceStateStopped  SourceState = "stopped"
)

// Capabilities describes guarantees provided by a source. The common API does
// not infer PostgreSQL or SQLite semantics from the source kind.
type Capabilities struct {
	Snapshot               bool `json:"snapshot,omitempty"`
	Durable                bool `json:"durable,omitempty"`
	Replayable             bool `json:"replayable,omitempty"`
	CapturesExternalWrites bool `json:"captures_external_writes,omitempty"`
	BeforeImages           bool `json:"before_images,omitempty"`
	Coalesced              bool `json:"coalesced,omitempty"`
}

// Stream is the driver-neutral event stream. Err is intentionally optional so
// existing stream implementations remain source-compatible; new sources may
// implement ErrStream to expose a terminal cause without synthesizing an
// error-valued row.
type Stream interface {
	Changes() <-chan Change
	Close()
}

// ErrStream exposes a terminal stream error. A consumer should type-assert
// this interface after Changes is closed.
type ErrStream interface {
	Stream
	Err() error
}

// Source is the common source contract implemented by every CDC driver.
// Subscribe receives a context so a source can reject subscriptions while it
// is not ready and can bind snapshot work to the caller's lifetime.
type Source interface {
	Info() SourceInfo
	Subscribe(context.Context, StreamOptions) (Stream, error)
}

// Registry is the read-only system-level CDC registry exposed to services and
// runtimes. Registry IDs, rather than driver aliases such as PostgreSQL slots,
// are the only global identity.
type Registry interface {
	List() []SourceInfo
	Get(registry.ID) (Source, bool)
}

type SourceInfo struct {
	ID           registry.ID   `json:"id,omitempty"`
	Kind         registry.Kind `json:"kind,omitempty"`
	State        SourceState   `json:"state,omitempty"`
	Capabilities Capabilities  `json:"capabilities,omitempty"`
	Generation   string        `json:"generation,omitempty"`

	// The fields below are retained for wire compatibility with existing Lua
	// and API consumers. New code must use ID, Kind, State, Capabilities and
	// Generation; driver-specific metadata should not be added to the common
	// contract.
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

var registryKey = &ctxapi.Key{Name: "cdc.registry"}

// WithRegistry attaches the driver-neutral CDC registry to the application
// context. Like the network and resource APIs, this is a write-once boot
// dependency and is safe to read after the application context is sealed.
func WithRegistry(ctx context.Context, registry Registry) context.Context {
	if registry == nil {
		return ctx
	}
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(registryKey) == nil {
		ac.With(registryKey, registry)
	}
	return ctx
}

// GetRegistry retrieves the system CDC registry from the application context.
func GetRegistry(ctx context.Context) Registry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	registry, _ := ac.Get(registryKey).(Registry)
	return registry
}
