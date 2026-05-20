// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"strconv"
	"strings"
)

func normalizePlaceholders(dbType string, query string) string {
	if mapDBTypeFromResourceKind(dbType) != TypePostgres {
		return query
	}
	return questionToDollarPlaceholders(query)
}

func questionToDollarPlaceholders(query string) string {
	if !strings.Contains(query, "?") {
		return query
	}

	var b strings.Builder
	b.Grow(len(query) + 8)
	index := 1

	for i := 0; i < len(query); {
		ch := query[i]

		if ch == '\'' {
			i = copySingleQuoted(&b, query, i)
			continue
		}
		if ch == '"' {
			i = copyDoubleQuoted(&b, query, i)
			continue
		}
		if ch == '-' && i+1 < len(query) && query[i+1] == '-' {
			i = copyLineComment(&b, query, i)
			continue
		}
		if ch == '/' && i+1 < len(query) && query[i+1] == '*' {
			i = copyBlockComment(&b, query, i)
			continue
		}
		if ch == '$' {
			if end, ok := dollarQuoteEnd(query, i); ok {
				b.WriteString(query[i:end])
				i = end
				continue
			}
		}
		if ch == '?' {
			if i+1 < len(query) && (query[i+1] == '|' || query[i+1] == '&') {
				b.WriteByte(ch)
				i++
				continue
			}
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(index))
			index++
			i++
			continue
		}

		b.WriteByte(ch)
		i++
	}

	return b.String()
}

func copySingleQuoted(b *strings.Builder, query string, start int) int {
	b.WriteByte(query[start])
	i := start + 1
	for i < len(query) {
		b.WriteByte(query[i])
		if query[i] == '\'' {
			if i+1 < len(query) && query[i+1] == '\'' {
				b.WriteByte(query[i+1])
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return i
}

func copyDoubleQuoted(b *strings.Builder, query string, start int) int {
	b.WriteByte(query[start])
	i := start + 1
	for i < len(query) {
		b.WriteByte(query[i])
		if query[i] == '"' {
			if i+1 < len(query) && query[i+1] == '"' {
				b.WriteByte(query[i+1])
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return i
}

func copyLineComment(b *strings.Builder, query string, start int) int {
	i := start
	for i < len(query) {
		b.WriteByte(query[i])
		if query[i] == '\n' {
			return i + 1
		}
		i++
	}
	return i
}

func copyBlockComment(b *strings.Builder, query string, start int) int {
	i := start
	for i < len(query) {
		b.WriteByte(query[i])
		if query[i] == '*' && i+1 < len(query) && query[i+1] == '/' {
			b.WriteByte(query[i+1])
			return i + 2
		}
		i++
	}
	return i
}

func dollarQuoteEnd(query string, start int) (int, bool) {
	i := start + 1
	for i < len(query) {
		ch := query[i]
		if ch == '$' {
			tag := query[start : i+1]
			end := strings.Index(query[i+1:], tag)
			if end < 0 {
				return 0, false
			}
			return i + 1 + end + len(tag), true
		}
		if !(ch == '_' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9') {
			return 0, false
		}
		i++
	}
	return 0, false
}
