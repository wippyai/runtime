// SPDX-License-Identifier: MPL-2.0

package ostime

import (
	"testing"

	"github.com/wippyai/go-lua/types/typ"
)

func TestDateOverloads(t *testing.T) {
	u, ok := dateFuncType.(*typ.Union)
	if !ok || len(u.Members) != 2 {
		t.Fatalf("os.date must be a two-member overload, got %s", typ.FormatShort(dateFuncType))
	}
	var tableForm, stringForm *typ.Function
	for _, m := range u.Members {
		fn, ok := m.(*typ.Function)
		if !ok {
			t.Fatalf("overload member is not a function: %s", typ.FormatShort(m))
		}
		if typ.TypeEquals(fn.Returns[0], typ.String) {
			stringForm = fn
		} else {
			tableForm = fn
		}
	}
	if tableForm == nil || stringForm == nil {
		t.Fatal("os.date must have a date-table form and a string form")
	}
	if tableForm.Params[0].Optional {
		t.Error(`the date-table form requires the "*t" format`)
	}
	if !stringForm.Params[0].Optional {
		t.Error("the string form accepts a call without a format")
	}
}
