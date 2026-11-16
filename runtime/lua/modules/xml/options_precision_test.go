// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

func TestMaxBytesIntegerLimitIsExact(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	const maximum = int64(1 << 53)
	for _, test := range []struct {
		value lua.LValue
		name  string
		valid bool
	}{
		{name: "integer boundary", value: lua.LInteger(maximum), valid: true},
		{name: "integer above boundary", value: lua.LInteger(maximum + 1)},
		{name: "integer two above boundary", value: lua.LInteger(maximum + 2)},
		{name: "float boundary", value: lua.LNumber(maximum), valid: true},
		{name: "float above boundary", value: lua.LNumber(maximum + 2)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := integerOption(l, "max_bytes", test.value, 0)
			if test.valid {
				require.Nil(t, err)
				require.Equal(t, maximum, got)
			} else {
				require.NotNil(t, err, "a value outside the limit must not round into range")
			}
		})
	}
}
