// SPDX-License-Identifier: MPL-2.0

package schema

import (
	"embed"

	schemamanager "github.com/wippyai/runtime/system/schema"
)

const (
	BundleName         = "registry_history"
	InitialVersion     = "1.0"
	sqliteSchemaPath   = "sqlite/registry/schema.sql"
	postgresSchemaPath = "postgres/registry/schema.sql"
)

//go:embed sqlite/registry/schema.sql postgres/registry/schema.sql
var assets embed.FS

func SQLiteBundle() schemamanager.Bundle {
	return schemamanager.Bundle{
		FS:             assets,
		Name:           BundleName,
		InitialVersion: InitialVersion,
		SchemaPath:     sqliteSchemaPath,
	}
}

func PostgresBundle() schemamanager.Bundle {
	return schemamanager.Bundle{
		FS:             assets,
		Name:           BundleName,
		InitialVersion: InitialVersion,
		SchemaPath:     postgresSchemaPath,
	}
}
