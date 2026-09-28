// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lib/pq"
	config "github.com/wippyai/runtime/api/service/cdc"
)

// PostgreSQL stores ordinary identifiers in NameData, whose default
// NAMEDATALEN leaves 63 bytes for the identifier. Replication slot and
// publication names are identifiers in the replication command grammar.
const postgresIdentifierMaxBytes = 63

// PostgreSQL replication slot names use a narrower grammar than ordinary
// identifiers. The server validates them as lowercase ASCII names composed
// only of letters, digits, and underscores; quoting does not broaden that
// rule. Keep this check separate from publication/table identifier quoting.
func quoteReplicationSlotName(value string) (string, error) {
	if value == "" || len(value) > postgresIdentifierMaxBytes {
		return "", fmt.Errorf("%w: slot_name", ErrInvalidIdentifier)
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return "", fmt.Errorf("%w: slot_name", ErrInvalidIdentifier)
		}
	}
	return pq.QuoteIdentifier(value), nil
}

func quotePostgresIdentifier(value, field string) (string, error) {
	if value == "" || value != strings.TrimSpace(value) ||
		len(value) > postgresIdentifierMaxBytes || !utf8.ValidString(value) {
		return "", fmt.Errorf("%w: %s", ErrInvalidIdentifier, field)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: %s", ErrInvalidIdentifier, field)
		}
	}
	return pq.QuoteIdentifier(value), nil
}

func validatePostgresIdentifier(value, field string) error {
	_, err := quotePostgresIdentifier(value, field)
	return err
}

// validateConfigIdentifiers applies the same SQL-grammar checks to both the
// driver-backed source and the retained legacy manager path. Keeping this
// policy in one helper prevents either construction path from creating an
// auto-publication before rejecting its slot name.
func validateConfigIdentifiers(cfg *config.Config) error {
	if _, err := quoteReplicationSlotName(cfg.SlotName); err != nil {
		return err
	}
	if cfg.Publication != "" {
		return validatePostgresIdentifier(cfg.Publication, "publication")
	}
	for _, table := range cfg.Tables {
		if _, err := quoteQualifiedIdent(table); err != nil {
			return err
		}
	}
	_, err := quotePostgresIdentifier(cfg.SlotName+"_pub", "publication")
	return err
}

func quotePostgresLiteral(value, field string) (string, error) {
	if value == "" || !utf8.ValidString(value) {
		return "", fmt.Errorf("%w: %s", ErrInvalidIdentifier, field)
	}
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return "", fmt.Errorf("%w: %s", ErrInvalidIdentifier, field)
		}
	}
	return pq.QuoteLiteral(value), nil
}
