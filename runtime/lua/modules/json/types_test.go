// SPDX-License-Identifier: MPL-2.0

package json

import (
	"strings"
	"testing"

	"github.com/wippyai/go-lua/compiler/check/tests/testutil"
)

func TestDecodeTypeNarrowsAfterErrorCheck(t *testing.T) {
	options := []testutil.Option{testutil.WithStdlib(), testutil.WithManifest("json", ModuleTypes())}
	source := `
		local json = require("json")
		type User = {id: string}
		local function read(s: string)
			local u, err = json.decode(s, User)
			if err then return end
			local id: string = u.id
			return id
		end
	`
	if result := testutil.Check(source, options...); result.HasError() {
		for _, d := range result.Errors {
			t.Log(d.Message)
		}
		t.Fatal("typed decode failed to narrow to User")
	}
	bad := strings.Replace(source, "local id: string = u.id", "local id: number = u.id", 1)
	if result := testutil.Check(bad, options...); !result.HasError() {
		t.Fatal("u.id must be string rather than any")
	}
}
