// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"testing"

	"github.com/wippyai/go-lua/types/typ"
)

func TestMessageHeaderTypeReturnsString(t *testing.T) {
	manifest := ModuleTypes()
	message, ok := manifest.LookupType("Message")
	if !ok {
		t.Fatal("Message type is not defined")
	}

	for _, method := range message.(*typ.Interface).Methods {
		if method.Name != "header" {
			continue
		}
		if len(method.Type.Returns) != 2 {
			t.Fatalf("header returns %d values, want value and error", len(method.Type.Returns))
		}
		if !typ.TypeEquals(method.Type.Returns[0], typ.String) {
			t.Fatalf("header value = %s, want string", method.Type.Returns[0])
		}
		return
	}

	t.Fatal("header method is not defined")
}
