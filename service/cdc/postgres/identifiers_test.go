// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotePostgresIdentifierRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{"", " slot", "slot ", "slot\nname", string([]byte{0xff})} {
		_, err := quotePostgresIdentifier(name, "slot_name")
		assert.ErrorIs(t, err, ErrInvalidIdentifier, "name %q", name)
	}
	_, err := quotePostgresIdentifier(strings.Repeat("x", postgresIdentifierMaxBytes+1), "slot_name")
	assert.ErrorIs(t, err, ErrInvalidIdentifier)
}

func TestQuotePostgresIdentifierUsesServerIdentifierQuoting(t *testing.T) {
	quoted, err := quotePostgresIdentifier(`slot"name`, "slot_name")
	require.NoError(t, err)
	assert.Equal(t, `"slot""name"`, quoted)

	literal, err := quotePostgresLiteral(`publication'name`, "publication")
	require.NoError(t, err)
	assert.Equal(t, `'publication''name'`, literal)

	qualified, err := quoteQualifiedIdent("public.accounts")
	require.NoError(t, err)
	assert.Equal(t, `"public"."accounts"`, qualified)
}

func TestQuoteQualifiedIdentRejectsMalformedTable(t *testing.T) {
	for _, table := range []string{"public.accounts.extra", ".accounts", "public."} {
		_, err := quoteQualifiedIdent(table)
		assert.ErrorIs(t, err, ErrInvalidIdentifier, "table %q", table)
	}
}
