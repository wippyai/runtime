// SPDX-License-Identifier: MPL-2.0

package schema

import (
	"embed"

	schemamanager "github.com/wippyai/runtime/system/schema"
)

const (
	BundleName            = "registry_history"
	InitialVersion        = "1.0"
	sqliteSchemaPath      = "sqlite/registry/schema.sql"
	postgresSchemaPath    = "postgres/registry/schema.sql"
	sqliteVersionedPath   = "sqlite/registry/versioned"
	postgresVersionedPath = "postgres/registry/versioned"
)

//go:embed sqlite/registry/schema.sql sqlite/registry/versioned/*/* postgres/registry/schema.sql postgres/registry/versioned/*/*
var assets embed.FS

func SQLiteBundle() schemamanager.Bundle {
	return schemamanager.Bundle{
		FS:             assets,
		Name:           BundleName,
		InitialVersion: InitialVersion,
		SchemaPath:     sqliteSchemaPath,
		VersionedPath:  sqliteVersionedPath,
	}
}

func PostgresBundle() schemamanager.Bundle {
	return schemamanager.Bundle{
		FS:             assets,
		Name:           BundleName,
		InitialVersion: InitialVersion,
		SchemaPath:     postgresSchemaPath,
		VersionedPath:  postgresVersionedPath,
	}
}
