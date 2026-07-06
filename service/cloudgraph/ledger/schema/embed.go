// SPDX-License-Identifier: MPL-2.0

// Package schema embeds the cloud-graph ledger schema bundle.
package schema

import (
	"embed"

	schemamanager "github.com/wippyai/runtime/system/schema"
)

const (
	// BundleName is the logical schema bundle name recorded in the version table.
	BundleName = "cloudgraph_ledger"
	// InitialVersion is the first schema version of the bundle.
	InitialVersion = "1.0"

	sqliteSchemaPath = "sqlite/cloudgraph/schema.sql"
)

//go:embed sqlite/cloudgraph/schema.sql
var assets embed.FS

// SQLiteBundle returns the SQLite schema bundle for the ledger.
func SQLiteBundle() schemamanager.Bundle {
	return schemamanager.Bundle{
		FS:             assets,
		Name:           BundleName,
		InitialVersion: InitialVersion,
		SchemaPath:     sqliteSchemaPath,
	}
}
