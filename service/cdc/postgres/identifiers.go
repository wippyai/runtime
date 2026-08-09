// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lib/pq"
)

// PostgreSQL stores ordinary identifiers in NameData, whose default
// NAMEDATALEN leaves 63 bytes for the identifier. Replication slot and
// publication names are identifiers in the replication command grammar.
const postgresIdentifierMaxBytes = 63

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
