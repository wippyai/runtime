// SPDX-License-Identifier: MPL-2.0

// Package storage implements registry storage migrations for durable backends.
package storage

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

const (
	entryMetadataLedgerName    = "registry_history.entry_metadata"
	entryMetadataLedgerVersion = "1.1"
	entryMetadataLedgerBase    = "1.0"
	entryMetadataDescription   = "move registry entry ownership into registry metadata"
	// SHA-256 of the canonical {curr_version,min_compatible_version,description}
	// migration manifest represented by these constants.
	entryMetadataLedgerHash = "798c77a8e5212cd0662c6b56395ee03b9f3291c0bab12c3d5df48bae7936738f"
)

// Tables names the already-managed history and schema-ledger tables for one
// durable backend.
type Tables struct {
	ChangeSets    string
	SchemaVersion string
	UpdateHistory string
}

// RewriteEntryMetadata rewrites every persisted changeset in one transaction.
// table names are supplied by the history backend after it has validated and
// quoted them for its dialect.
func RewriteEntryMetadata(
	ctx context.Context,
	db *sql.DB,
	handle *codec.MsgpackHandle,
	tables Tables,
	parameter func(int) string,
	lock func(context.Context, *sql.Tx) error,
	baseline registry.State,
) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin entry metadata migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if lock != nil {
		if err := lock(ctx, tx); err != nil {
			return fmt.Errorf("lock entry metadata migration: %w", err)
		}
	}

	current, exists, err := readVersion(ctx, tx, tables.SchemaVersion, parameter)
	if err != nil {
		return err
	}
	if exists {
		switch current {
		case entryMetadataLedgerVersion:
			return nil
		case entryMetadataLedgerBase:
		default:
			return fmt.Errorf("unsupported entry metadata migration version %q", current)
		}
	}

	baselineMetadata := make(map[registry.ID]registry.EntryMetadata, len(baseline))
	for _, entry := range baseline {
		baselineMetadata[entry.ID.Canonical()] = entry.Registry
	}

	// #nosec G202 -- table names are fixed by the backend constructor, not input.
	rows, err := tx.QueryContext(ctx, "SELECT version_id, data FROM "+tables.ChangeSets+" ORDER BY version_id")
	if err != nil {
		return fmt.Errorf("read registry changesets: %w", err)
	}
	type row struct {
		data      []byte
		versionID uint
	}
	var stored []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.versionID, &item.data); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan registry changeset: %w", err)
		}
		stored = append(stored, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("read registry changesets: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close registry changesets: %w", err)
	}

	// #nosec G202 -- table names are fixed by the backend constructor, not input.
	updateQuery := "UPDATE " + tables.ChangeSets + " SET data = " + parameter(1) + " WHERE version_id = " + parameter(2)
	for _, item := range stored {
		data, changed, err := rewriteChangeSet(item.data, handle, baselineMetadata)
		if err != nil {
			return fmt.Errorf("rewrite registry changeset %d: %w", item.versionID, err)
		}
		if !changed {
			continue
		}
		if _, err := tx.ExecContext(ctx, updateQuery, data, item.versionID); err != nil {
			return fmt.Errorf("store registry changeset %d: %w", item.versionID, err)
		}
	}
	if err := writeVersion(ctx, tx, tables, parameter, exists, current); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit entry metadata migration: %w", err)
	}
	return nil
}

func readVersion(ctx context.Context, tx *sql.Tx, table string, parameter func(int) string) (string, bool, error) {
	var current string
	err := tx.QueryRowContext(ctx, "SELECT curr_version FROM "+table+" WHERE name = "+parameter(1), entryMetadataLedgerName).Scan(&current)
	if err == sql.ErrNoRows {
		return entryMetadataLedgerBase, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read entry metadata migration version: %w", err)
	}
	return current, true, nil
}

func writeVersion(ctx context.Context, tx *sql.Tx, tables Tables, parameter func(int) string, exists bool, current string) error {
	old := entryMetadataLedgerBase
	if exists {
		old = current
	}
	now := time.Now().UTC()
	// #nosec G202 -- table names are fixed by the backend constructor, not input.
	versionQuery := "INSERT INTO " + tables.SchemaVersion + " (name, curr_version, min_compatible_version, updated_at) VALUES (" + parameter(1) + ", " + parameter(2) + ", " + parameter(3) + ", " + parameter(4) + ")"
	if parameter(1) == "?" {
		versionQuery += " ON CONFLICT(name) DO UPDATE SET curr_version = excluded.curr_version, min_compatible_version = excluded.min_compatible_version, updated_at = excluded.updated_at"
	} else {
		versionQuery += " ON CONFLICT(name) DO UPDATE SET curr_version = EXCLUDED.curr_version, min_compatible_version = EXCLUDED.min_compatible_version, updated_at = EXCLUDED.updated_at"
	}
	if _, err := tx.ExecContext(ctx, versionQuery, entryMetadataLedgerName, entryMetadataLedgerVersion, entryMetadataLedgerBase, now); err != nil {
		return fmt.Errorf("write entry metadata migration version: %w", err)
	}
	// #nosec G202 -- table names are fixed by the backend constructor, not input.
	auditQuery := "INSERT INTO " + tables.UpdateHistory + " (name, update_time, old_version, new_version, manifest_sha256, description) VALUES (" + parameter(1) + ", " + parameter(2) + ", " + parameter(3) + ", " + parameter(4) + ", " + parameter(5) + ", " + parameter(6) + ")"
	if _, err := tx.ExecContext(ctx, auditQuery, entryMetadataLedgerName, now, old, entryMetadataLedgerVersion, entryMetadataLedgerHash, entryMetadataDescription); err != nil {
		return fmt.Errorf("write entry metadata migration history: %w", err)
	}
	return nil
}

type encodedPayload struct {
	Data   any
	Format payload.Format
}

// encodedEntry accepts every released storage shape. DependencyRoot and the
// three metadata stamps existed only in persisted rows; normal history codecs
// do not read or write them.
type encodedEntry struct {
	Meta           attrs.Bag
	Data           *encodedPayload
	ID             registry.ID
	Kind           string
	Registry       registry.EntryMetadata
	DependencyRoot bool `codec:"DependencyRoot,omitempty"`
}

type releasedOwnership struct {
	Module  string
	Version string
	Digest  string
	Root    bool
}

// OwnershipDecodeError prevents a present but unrecognized provenance map
// from being interpreted as an explicit false deployment root.
type OwnershipDecodeError struct {
	ID     registry.ID
	Field  string
	Reason string
}

func (e *OwnershipDecodeError) Error() string {
	return fmt.Sprintf("decode ownership %s %s: %s", e.ID.String(), e.Field, e.Reason)
}

type encodedOperation struct {
	OriginalEntry *encodedEntry  `codec:"OriginalEntry"`
	Current       map[string]any `codec:"prov,omitempty"`
	Previous      map[string]any `codec:"oprov,omitempty"`
	Kind          string         `codec:"Kind"`
	Entry         encodedEntry   `codec:"Entry"`
}

func decodeOwnership(id registry.ID, field string, raw map[string]any) (*releasedOwnership, error) {
	if raw == nil {
		return nil, nil
	}
	record := &releasedOwnership{}
	known := false
	for _, names := range []struct {
		lower string
		upper string
		to    *string
	}{{"module", "Module", &record.Module}, {"version", "Version", &record.Version}, {"digest", "Digest", &record.Digest}} {
		lower, hasLower := raw[names.lower]
		upper, hasUpper := raw[names.upper]
		if !hasLower && !hasUpper {
			continue
		}
		known = true
		if hasLower {
			value, ok := lower.(string)
			if !ok {
				return nil, &OwnershipDecodeError{ID: id, Field: field, Reason: names.lower + " must be a string"}
			}
			*names.to = value
		}
		if hasUpper {
			value, ok := upper.(string)
			if !ok {
				return nil, &OwnershipDecodeError{ID: id, Field: field, Reason: names.upper + " must be a string"}
			}
			if hasLower && *names.to != value {
				return nil, &OwnershipDecodeError{ID: id, Field: field, Reason: "conflicting " + names.lower + " keys"}
			}
			*names.to = value
		}
	}
	for _, key := range []string{"root", "Root"} {
		value, exists := raw[key]
		if !exists {
			continue
		}
		known = true
		root, ok := value.(bool)
		if !ok {
			return nil, &OwnershipDecodeError{ID: id, Field: field, Reason: key + " must be a bool"}
		}
		if key == "Root" {
			if lower, exists := raw["root"]; exists && lower != root {
				return nil, &OwnershipDecodeError{ID: id, Field: field, Reason: "conflicting root keys"}
			}
		}
		record.Root = root
	}
	// Released host-authored records can legitimately be an empty map because
	// every field was optional. A nonempty map with no released keys is corrupt.
	if !known && len(raw) != 0 {
		return nil, &OwnershipDecodeError{ID: id, Field: field, Reason: "no released ownership keys"}
	}
	return record, nil
}

func rewriteChangeSet(data []byte, handle *codec.MsgpackHandle, baseline map[registry.ID]registry.EntryMetadata) ([]byte, bool, error) {
	var wire []map[string]any
	if err := codec.NewDecoder(bytes.NewReader(data), handle).Decode(&wire); err != nil {
		return nil, false, &OwnershipDecodeError{Reason: err.Error()}
	}
	var operations []encodedOperation
	decoder := codec.NewDecoder(bytes.NewReader(data), handle)
	if err := decoder.Decode(&operations); err != nil {
		return nil, false, &OwnershipDecodeError{Reason: err.Error()}
	}

	changed := false
	for i := range operations {
		op := &operations[i]
		for _, field := range []struct {
			name   string
			record map[string]any
		}{{"prov", op.Current}, {"oprov", op.Previous}} {
			if _, present := wire[i][field.name]; present && field.record == nil {
				return nil, false, &OwnershipDecodeError{ID: op.Entry.ID, Field: field.name, Reason: "record is null or not an ownership map"}
			}
		}
		current, err := decodeOwnership(op.Entry.ID, "prov", op.Current)
		if err != nil {
			return nil, false, err
		}
		previous, err := decodeOwnership(op.Entry.ID, "oprov", op.Previous)
		if err != nil {
			return nil, false, err
		}
		if err := validateOperationOwners(op, current, previous); err != nil {
			return nil, false, err
		}
		currentOwner := storedOwner(&op.Entry, current)
		previousOwner := ""
		if op.OriginalEntry != nil {
			previousOwner = storedOwner(op.OriginalEntry, previous)
		}
		entryChanged, err := rewriteEntry(&op.Entry, current, previousOwner, baseline)
		if err != nil {
			return nil, false, err
		}
		changed = entryChanged || changed
		if op.OriginalEntry != nil {
			originalChanged, err := rewriteEntry(op.OriginalEntry, previous, currentOwner, baseline)
			if err != nil {
				return nil, false, err
			}
			changed = originalChanged || changed
		}
		if op.Current != nil || op.Previous != nil {
			op.Current = nil
			op.Previous = nil
			changed = true
		}
	}
	if !changed {
		return data, false, nil
	}

	var out bytes.Buffer
	encoder := codec.NewEncoder(&out, handle)
	if err := encoder.Encode(operations); err != nil {
		return nil, false, err
	}
	return out.Bytes(), true, nil
}

func validateOperationOwners(operation *encodedOperation, current, previous *releasedOwnership) error {
	if err := validateEntryOwner(&operation.Entry, current, "prov"); err != nil {
		return err
	}
	if operation.OriginalEntry == nil {
		return nil
	}
	if err := validateEntryOwner(operation.OriginalEntry, previous, "oprov"); err != nil {
		return err
	}
	currentOwner := storedOwner(&operation.Entry, current)
	previousOwner := storedOwner(operation.OriginalEntry, previous)
	if currentOwner != "" && previousOwner != "" && currentOwner != previousOwner {
		return &OwnershipDecodeError{ID: operation.Entry.ID.Canonical(), Field: "prov/oprov",
			Reason: fmt.Sprintf("conflicting owners %q and %q", previousOwner, currentOwner)}
	}
	return nil
}

func validateEntryOwner(entry *encodedEntry, record *releasedOwnership, field string) error {
	if record == nil || record.Module == "" || entry.Registry.Owner == "" || record.Module == entry.Registry.Owner {
		return nil
	}
	return &OwnershipDecodeError{ID: entry.ID.Canonical(), Field: field,
		Reason: fmt.Sprintf("conflicting owners %q and %q", entry.Registry.Owner, record.Module)}
}

func rewriteEntry(entry *encodedEntry, record *releasedOwnership, pairedOwner string, baseline map[registry.ID]registry.EntryMetadata) (bool, error) {
	changed := false
	id := entry.ID.Canonical()
	if id != entry.ID {
		entry.ID = id
		changed = true
	}

	metadata := entry.Registry
	hadMetadata := metadata != (registry.EntryMetadata{})
	base, hasBaseline := baseline[id]
	owner := storedOwner(entry, record)
	if owner == "" {
		owner = pairedOwner
	}
	if hasBaseline && base.Owner != "" {
		if owner != "" && owner != base.Owner {
			return false, &OwnershipDecodeError{ID: id, Field: "baseline",
				Reason: fmt.Sprintf("conflicting owners %q and %q", owner, base.Owner)}
		}
		owner = base.Owner
	}
	if metadata.Owner != owner {
		metadata.Owner = owner
		changed = true
	}

	// A persisted ownership record or dependency-root bit is an explicit state
	// change. Otherwise keep the active baseline selection for the entry.
	if record != nil {
		if metadata.Root != record.Root {
			metadata.Root = record.Root
			changed = true
		}
	} else if entry.DependencyRoot {
		if !metadata.Root {
			metadata.Root = true
			changed = true
		}
	} else if !hadMetadata && hasBaseline && metadata.Root != base.Root {
		metadata.Root = base.Root
		changed = true
	}
	if entry.DependencyRoot {
		entry.DependencyRoot = false
		changed = true
	}
	if entry.Registry != metadata {
		entry.Registry = metadata
		changed = true
	}
	for _, key := range []string{"module", "module_version", "module_digest"} {
		if _, ok := entry.Meta[key]; ok {
			delete(entry.Meta, key)
			changed = true
		}
	}
	return changed, nil
}

func storedOwner(entry *encodedEntry, record *releasedOwnership) string {
	if entry.Registry.Owner != "" {
		return entry.Registry.Owner
	}
	if record != nil && record.Module != "" {
		return record.Module
	}
	owner, _ := entry.Meta["module"].(string)
	return owner
}
