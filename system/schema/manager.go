// SPDX-License-Identifier: MPL-2.0

package schema

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

type Dialect string

const (
	DialectPostgres Dialect = "postgres"
	DialectSQLite   Dialect = "sqlite"
)

type Bundle struct {
	FS             fs.FS
	Name           string
	InitialVersion string
	SchemaPath     string
	VersionedPath  string
}

type Target struct {
	DB          *sql.DB
	Dialect     Dialect
	LogicalName string
	SchemaName  string
}

type Manager struct {
	bundle Bundle
	target Target
}

type manifest struct {
	CurrVersion          string `json:"curr_version"`
	MinCompatibleVersion string `json:"min_compatible_version"`
	Description          string `json:"description"`
	dir                  string
	manifestHash         string
	SchemaUpdateFiles    []string `json:"schema_update_files"`
}

type sqlExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type sqlQueryer interface {
	sqlExecer
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func NewManager(bundle Bundle, target Target) (*Manager, error) {
	if bundle.Name == "" {
		return nil, fmt.Errorf("schema bundle name is required")
	}
	if bundle.FS == nil {
		return nil, fmt.Errorf("schema bundle filesystem is required")
	}
	if bundle.InitialVersion == "" {
		return nil, fmt.Errorf("schema bundle initial version is required")
	}
	if bundle.SchemaPath == "" {
		return nil, fmt.Errorf("schema bundle schema path is required")
	}
	if target.DB == nil {
		return nil, fmt.Errorf("schema target database is required")
	}
	if target.LogicalName == "" {
		return nil, fmt.Errorf("schema target logical name is required")
	}
	switch target.Dialect {
	case DialectSQLite:
	case DialectPostgres:
		if target.SchemaName == "" {
			return nil, fmt.Errorf("schema name is required for postgres targets")
		}
		if err := ValidateIdentifier(target.SchemaName); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported schema dialect %q", target.Dialect)
	}

	return &Manager{bundle: bundle, target: target}, nil
}

func (m *Manager) Setup(ctx context.Context) error {
	if m.target.Dialect == DialectPostgres {
		return m.setupPostgres(ctx)
	}

	if err := m.createVersionTables(ctx, m.target.DB); err != nil {
		return err
	}

	_, exists, err := m.readVersion(ctx)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	tx, err := m.target.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema setup: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := m.applyInitialSchema(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema setup: %w", err)
	}
	return nil
}

func (m *Manager) setupPostgres(ctx context.Context) error {
	tx, err := m.target.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin postgres schema setup: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := m.lockPostgresSchema(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+QuoteIdentifier(m.target.SchemaName)); err != nil {
		return fmt.Errorf("create postgres schema: %w", err)
	}
	if err := m.createVersionTables(ctx, tx); err != nil {
		return err
	}

	_, exists, err := m.readVersionFrom(ctx, tx)
	if err != nil {
		return err
	}
	if exists {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit postgres schema setup: %w", err)
		}
		return nil
	}

	if err := m.applyInitialSchema(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit postgres schema setup: %w", err)
	}
	return nil
}

func (m *Manager) applyInitialSchema(ctx context.Context, execer sqlExecer) error {
	schemaBytes, err := fs.ReadFile(m.bundle.FS, m.bundle.SchemaPath)
	if err != nil {
		return err
	}
	sqlText, err := renderSQL(schemaBytes, m.target)
	if err != nil {
		return err
	}
	if err := execSQL(ctx, execer, sqlText); err != nil {
		return fmt.Errorf("apply initial schema %s: %w", m.bundle.Name, err)
	}
	if err := m.writeVersion(ctx, execer, "", m.bundle.InitialVersion, m.bundle.InitialVersion, "initial schema", hashBytes(schemaBytes)); err != nil {
		return err
	}
	return nil
}

func (m *Manager) Update(ctx context.Context) error {
	if m.bundle.VersionedPath == "" {
		return nil
	}
	if m.target.Dialect == DialectPostgres {
		return m.updatePostgres(ctx)
	}
	current, exists, err := m.readVersion(ctx)
	if err != nil {
		return err
	}
	if !exists {
		if err := m.Setup(ctx); err != nil {
			return err
		}
		current = m.bundle.InitialVersion
	}

	manifests, err := m.loadManifests()
	if err != nil {
		return err
	}
	for _, mf := range manifests {
		if !versionGreater(mf.CurrVersion, current) {
			continue
		}
		if versionGreater(mf.MinCompatibleVersion, current) {
			return fmt.Errorf("schema %s version %s requires at least %s, current is %s", m.bundle.Name, mf.CurrVersion, mf.MinCompatibleVersion, current)
		}

		tx, err := m.target.DB.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin schema update %s: %w", mf.CurrVersion, err)
		}
		for _, file := range mf.SchemaUpdateFiles {
			updatePath := path.Join(mf.dir, file)
			updateBytes, err := fs.ReadFile(m.bundle.FS, updatePath)
			if err != nil {
				_ = tx.Rollback()
				return err
			}
			sqlText, err := renderSQL(updateBytes, m.target)
			if err != nil {
				_ = tx.Rollback()
				return err
			}
			if err := execSQL(ctx, tx, sqlText); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("apply schema update %s file %s: %w", mf.CurrVersion, file, err)
			}
		}
		if err := m.writeVersion(ctx, tx, current, mf.CurrVersion, mf.MinCompatibleVersion, mf.Description, mf.manifestHash); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit schema update %s: %w", mf.CurrVersion, err)
		}
		current = mf.CurrVersion
	}
	return nil
}

func (m *Manager) updatePostgres(ctx context.Context) error {
	tx, err := m.target.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin postgres schema update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := m.lockPostgresSchema(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+QuoteIdentifier(m.target.SchemaName)); err != nil {
		return fmt.Errorf("create postgres schema: %w", err)
	}
	if err := m.createVersionTables(ctx, tx); err != nil {
		return err
	}

	current, exists, err := m.readVersionFrom(ctx, tx)
	if err != nil {
		return err
	}
	if !exists {
		if err := m.applyInitialSchema(ctx, tx); err != nil {
			return err
		}
		current = m.bundle.InitialVersion
	}

	manifests, err := m.loadManifests()
	if err != nil {
		return err
	}
	for _, mf := range manifests {
		if !versionGreater(mf.CurrVersion, current) {
			continue
		}
		if versionGreater(mf.MinCompatibleVersion, current) {
			return fmt.Errorf("schema %s version %s requires at least %s, current is %s", m.bundle.Name, mf.CurrVersion, mf.MinCompatibleVersion, current)
		}
		for _, file := range mf.SchemaUpdateFiles {
			updatePath := path.Join(mf.dir, file)
			updateBytes, err := fs.ReadFile(m.bundle.FS, updatePath)
			if err != nil {
				return err
			}
			sqlText, err := renderSQL(updateBytes, m.target)
			if err != nil {
				return err
			}
			if err := execSQL(ctx, tx, sqlText); err != nil {
				return fmt.Errorf("apply schema update %s file %s: %w", mf.CurrVersion, file, err)
			}
		}
		if err := m.writeVersion(ctx, tx, current, mf.CurrVersion, mf.MinCompatibleVersion, mf.Description, mf.manifestHash); err != nil {
			return err
		}
		current = mf.CurrVersion
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit postgres schema update: %w", err)
	}
	return nil
}

func (m *Manager) CurrentVersion(ctx context.Context) (string, error) {
	version, exists, err := m.readVersion(ctx)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", nil
	}
	return version, nil
}

func (m *Manager) createVersionTables(ctx context.Context, execer sqlExecer) error {
	var statements string
	switch m.target.Dialect {
	case DialectSQLite:
		statements = `
CREATE TABLE IF NOT EXISTS schema_version (
	name TEXT PRIMARY KEY,
	curr_version TEXT NOT NULL,
	min_compatible_version TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS schema_update_history (
	name TEXT NOT NULL,
	update_time TEXT NOT NULL,
	old_version TEXT NOT NULL,
	new_version TEXT NOT NULL,
	manifest_sha256 TEXT NOT NULL,
	description TEXT,
	PRIMARY KEY (name, update_time, new_version)
);
`
	case DialectPostgres:
		schema := QuoteIdentifier(m.target.SchemaName)
		statements = fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s.schema_version (
	name TEXT PRIMARY KEY,
	curr_version TEXT NOT NULL,
	min_compatible_version TEXT NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS %s.schema_update_history (
	name TEXT NOT NULL,
	update_time TIMESTAMPTZ NOT NULL,
	old_version TEXT NOT NULL,
	new_version TEXT NOT NULL,
	manifest_sha256 TEXT NOT NULL,
	description TEXT,
	PRIMARY KEY (name, update_time, new_version)
);
`, schema, schema)
	default:
		return fmt.Errorf("unsupported schema dialect %q", m.target.Dialect)
	}
	if err := execSQL(ctx, execer, statements); err != nil {
		return fmt.Errorf("create schema version tables: %w", err)
	}
	return nil
}

func (m *Manager) readVersion(ctx context.Context) (string, bool, error) {
	return m.readVersionFrom(ctx, m.target.DB)
}

func (m *Manager) readVersionFrom(ctx context.Context, querier sqlQueryer) (string, bool, error) {
	query := "SELECT curr_version FROM schema_version WHERE name = ?"
	if m.target.Dialect == DialectPostgres {
		query = fmt.Sprintf("SELECT curr_version FROM %s.schema_version WHERE name = $1", QuoteIdentifier(m.target.SchemaName))
	}

	var version string
	err := querier.QueryRowContext(ctx, query, m.target.LogicalName).Scan(&version)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read schema version: %w", err)
	}
	return version, true, nil
}

func (m *Manager) writeVersion(ctx context.Context, execer sqlExecer, oldVersion, newVersion, minCompatibleVersion, description, manifestHash string) error {
	now := time.Now().UTC()

	switch m.target.Dialect {
	case DialectSQLite:
		if _, err := execer.ExecContext(ctx, `
INSERT INTO schema_version (name, curr_version, min_compatible_version, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
	curr_version = excluded.curr_version,
	min_compatible_version = excluded.min_compatible_version,
	updated_at = excluded.updated_at
`, m.target.LogicalName, newVersion, minCompatibleVersion, now.Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("write schema version: %w", err)
		}
		if _, err := execer.ExecContext(ctx, `
INSERT INTO schema_update_history (name, update_time, old_version, new_version, manifest_sha256, description)
VALUES (?, ?, ?, ?, ?, ?)
`, m.target.LogicalName, now.Format(time.RFC3339Nano), oldVersion, newVersion, manifestHash, description); err != nil {
			return fmt.Errorf("write schema update history: %w", err)
		}
	case DialectPostgres:
		schema := QuoteIdentifier(m.target.SchemaName)
		if _, err := execer.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO %s.schema_version (name, curr_version, min_compatible_version, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT(name) DO UPDATE SET
	curr_version = EXCLUDED.curr_version,
	min_compatible_version = EXCLUDED.min_compatible_version,
	updated_at = EXCLUDED.updated_at
`, schema), m.target.LogicalName, newVersion, minCompatibleVersion, now); err != nil {
			return fmt.Errorf("write schema version: %w", err)
		}
		if _, err := execer.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO %s.schema_update_history (name, update_time, old_version, new_version, manifest_sha256, description)
VALUES ($1, $2, $3, $4, $5, $6)
`, schema), m.target.LogicalName, now, oldVersion, newVersion, manifestHash, description); err != nil {
			return fmt.Errorf("write schema update history: %w", err)
		}
	default:
		return fmt.Errorf("unsupported schema dialect %q", m.target.Dialect)
	}
	return nil
}

func (m *Manager) lockPostgresSchema(ctx context.Context, execer sqlExecer) error {
	if _, err := execer.ExecContext(ctx,
		"SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))",
		m.bundle.Name+":"+m.target.LogicalName,
		m.target.SchemaName,
	); err != nil {
		return fmt.Errorf("lock postgres schema %s: %w", m.target.SchemaName, err)
	}
	return nil
}

func (m *Manager) loadManifests() ([]manifest, error) {
	var manifests []manifest
	err := fs.WalkDir(m.bundle.FS, m.bundle.VersionedPath, func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Base(filePath) != "manifest.json" {
			return nil
		}
		data, err := fs.ReadFile(m.bundle.FS, filePath)
		if err != nil {
			return err
		}
		var mf manifest
		if err := json.Unmarshal(data, &mf); err != nil {
			return fmt.Errorf("decode schema manifest %s: %w", filePath, err)
		}
		dir := path.Dir(filePath)
		dirVersion := strings.TrimPrefix(path.Base(dir), "v")
		if mf.CurrVersion != dirVersion {
			return fmt.Errorf("manifest version %s does not match directory %s", mf.CurrVersion, path.Base(dir))
		}
		if mf.MinCompatibleVersion == "" {
			return fmt.Errorf("schema manifest %s missing min_compatible_version", filePath)
		}
		mf.dir = dir
		mf.manifestHash = hashBytes(data)
		manifests = append(manifests, mf)
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	sort.Slice(manifests, func(i, j int) bool {
		return versionGreater(manifests[j].CurrVersion, manifests[i].CurrVersion)
	})
	return manifests, nil
}

func execSQL(ctx context.Context, execer sqlExecer, sqlText string) error {
	for _, stmt := range splitStatements(sqlText) {
		if _, err := execer.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func splitStatements(sqlText string) []string {
	var statements []string
	var b strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	for _, r := range sqlText {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inSingle:
			escaped = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case r == ';' && !inSingle && !inDouble:
			stmt := strings.TrimSpace(b.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			b.Reset()
			continue
		}
		b.WriteRune(r)
	}

	stmt := strings.TrimSpace(b.String())
	if stmt != "" {
		statements = append(statements, stmt)
	}
	return statements
}

func renderSQL(data []byte, target Target) (string, error) {
	sqlText := string(data)
	if target.Dialect == DialectSQLite {
		if strings.Contains(sqlText, "{{schema}}") {
			return "", fmt.Errorf("sqlite schema SQL must not contain {{schema}}")
		}
		return sqlText, nil
	}
	if target.Dialect == DialectPostgres {
		if target.SchemaName == "" {
			return "", fmt.Errorf("schema name is required for postgres targets")
		}
		if err := ValidateIdentifier(target.SchemaName); err != nil {
			return "", err
		}
		return strings.ReplaceAll(sqlText, "{{schema}}", QuoteIdentifier(target.SchemaName)), nil
	}
	return "", fmt.Errorf("unsupported schema dialect %q", target.Dialect)
}

func ValidateIdentifier(value string) error {
	if !identifierPattern.MatchString(value) {
		return fmt.Errorf("invalid schema name %q", value)
	}
	return nil
}

func QuoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func versionGreater(left, right string) bool {
	leftVersion, leftErr := semver.NewVersion(left)
	rightVersion, rightErr := semver.NewVersion(right)
	if leftErr == nil && rightErr == nil {
		return leftVersion.GreaterThan(rightVersion)
	}
	return left > right
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
