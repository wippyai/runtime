// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type columnInfo struct {
	name string
	text bool
}

func textAffinity(declType string) bool {
	t := strings.ToUpper(declType)
	if strings.Contains(t, "BLOB") || t == "" {
		return false
	}
	return strings.Contains(t, "CHAR") || strings.Contains(t, "CLOB") || strings.Contains(t, "TEXT")
}

func mapRow(cols []columnInfo, vals []any) map[string]any {
	if vals == nil {
		return nil
	}
	out := make(map[string]any, len(vals))
	for i, v := range vals {
		name, text := columnAt(cols, i)
		out[name] = normalizeValue(v, text)
	}
	return out
}

func columnAt(cols []columnInfo, i int) (string, bool) {
	if i < len(cols) {
		return cols[i].name, cols[i].text
	}
	return "column" + strconv.Itoa(i), false
}

func normalizeValue(v any, text bool) any {
	b, ok := v.([]byte)
	if !ok {
		return v
	}
	if text && utf8.Valid(b) {
		return string(b)
	}
	return b
}
