package cache

import (
	"time"

	"github.com/wippyai/go-lua/types/diag"
)

// SchemaVersion identifies the cache file layout.
const SchemaVersion = 1

// Entry represents a cached Lua artifact set.
type Entry struct {
	Manifest    []byte
	Diagnostics []diag.Diagnostic
	Proto       []byte
	Meta        Meta
}

// Store defines read/write behavior for cache entries.
type Store interface {
	Get(key string) (*Entry, bool, error)
	Put(key string, entry *Entry) error
}

// Deleter optionally removes cache entries by key.
type Deleter interface {
	Delete(key string) error
}

// Meta stores identifying information for an entry.
type Meta struct {
	CreatedAt            time.Time `json:"created_at"`
	CompileFingerprint   string    `json:"compile_fingerprint"`
	TypecheckFingerprint string    `json:"typecheck_fingerprint,omitempty"`
	EntryID              string    `json:"entry_id"`
	Kind                 string    `json:"kind"`
	Method               string    `json:"method,omitempty"`
	SourceHash           string    `json:"source_hash"`
	BuiltinHash          string    `json:"builtin_hash,omitempty"`
	TypecheckConfigHash  string    `json:"typecheck_config_hash,omitempty"`
	Deps                 []DepMeta `json:"deps,omitempty"`
	SchemaVersion        int       `json:"schema_version"`
}

// DepMeta records dependency fingerprints for debugging.
type DepMeta struct {
	Alias                string `json:"alias"`
	ID                   string `json:"id"`
	CompileFingerprint   string `json:"compile_fingerprint,omitempty"`
	TypecheckFingerprint string `json:"typecheck_fingerprint,omitempty"`
}
