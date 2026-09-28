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

type encodedOperation struct {
	OriginalEntry *encodedEntry      `codec:"OriginalEntry"`
	Current       *releasedOwnership `codec:"prov,omitempty"`
	Previous      *releasedOwnership `codec:"oprov,omitempty"`
	Kind          string             `codec:"Kind"`
	Entry         encodedEntry       `codec:"Entry"`
}

func rewriteChangeSet(data []byte, handle *codec.MsgpackHandle, baseline map[registry.ID]registry.EntryMetadata) ([]byte, bool, error) {
	var operations []encodedOperation
	decoder := codec.NewDecoder(bytes.NewReader(data), handle)
	if err := decoder.Decode(&operations); err != nil {
		return nil, false, err
	}

	changed := false
	for i := range operations {
		op := &operations[i]
		if err := validateOperationOwners(op); err != nil {
			return nil, false, err
		}
		currentOwner := storedOwner(&op.Entry, op.Current)
		previousOwner := ""
		if op.OriginalEntry != nil {
			previousOwner = storedOwner(op.OriginalEntry, op.Previous)
		}
		entryChanged, err := rewriteEntry(&op.Entry, op.Current, previousOwner, baseline)
		if err != nil {
			return nil, false, err
		}
		changed = entryChanged || changed
		if op.OriginalEntry != nil {
			originalChanged, err := rewriteEntry(op.OriginalEntry, op.Previous, currentOwner, baseline)
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

func validateOperationOwners(operation *encodedOperation) error {
	if err := validateEntryOwner(&operation.Entry, operation.Current); err != nil {
		return err
	}
	if operation.OriginalEntry == nil {
		return nil
	}
	if err := validateEntryOwner(operation.OriginalEntry, operation.Previous); err != nil {
		return err
	}
	current := storedOwner(&operation.Entry, operation.Current)
	previous := storedOwner(operation.OriginalEntry, operation.Previous)
	if current != "" && previous != "" && current != previous {
		return fmt.Errorf("%s has conflicting owners %q and %q", operation.Entry.ID.Canonical(), previous, current)
	}
	return nil
}

func validateEntryOwner(entry *encodedEntry, record *releasedOwnership) error {
	if record == nil || record.Module == "" || entry.Registry.Owner == "" || record.Module == entry.Registry.Owner {
		return nil
	}
	return fmt.Errorf("%s has conflicting owners %q and %q", entry.ID.Canonical(), entry.Registry.Owner, record.Module)
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
			return false, fmt.Errorf("%s has conflicting owners %q and %q", id, owner, base.Owner)
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
