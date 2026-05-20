// SPDX-License-Identifier: MPL-2.0

package sql

import "testing"

func TestNormalizePlaceholdersForPostgres(t *testing.T) {
	got := normalizePlaceholders("db.sql.postgres", "SELECT * FROM users WHERE id = ? AND status = ?")
	want := "SELECT * FROM users WHERE id = $1 AND status = $2"
	if got != want {
		t.Fatalf("unexpected normalized query\nwant: %s\n got: %s", want, got)
	}
}

func TestNormalizePlaceholdersLeavesSQLiteQuestionMarks(t *testing.T) {
	got := normalizePlaceholders("db.sql.sqlite", "SELECT * FROM users WHERE id = ?")
	want := "SELECT * FROM users WHERE id = ?"
	if got != want {
		t.Fatalf("unexpected sqlite query\nwant: %s\n got: %s", want, got)
	}
}

func TestNormalizePlaceholdersSkipsQuotedTextAndComments(t *testing.T) {
	query := "SELECT '?' AS literal, \"?\" AS ident -- ?\n/* ? */ WHERE id = ? AND body = $$?$$"
	got := normalizePlaceholders("db.sql.postgres", query)
	want := "SELECT '?' AS literal, \"?\" AS ident -- ?\n/* ? */ WHERE id = $1 AND body = $$?$$"
	if got != want {
		t.Fatalf("unexpected normalized query\nwant: %s\n got: %s", want, got)
	}
}

func TestNormalizePlaceholdersPreservesPostgresJsonbQuestionOperators(t *testing.T) {
	got := normalizePlaceholders("db.sql.postgres", "SELECT data ?| array['a'] FROM docs WHERE id = ? AND data ?& array['b']")
	want := "SELECT data ?| array['a'] FROM docs WHERE id = $1 AND data ?& array['b']"
	if got != want {
		t.Fatalf("unexpected normalized query\nwant: %s\n got: %s", want, got)
	}
}
