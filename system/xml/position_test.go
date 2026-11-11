// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestTextErrorPositions(t *testing.T) {
	for _, tc := range []struct{ name, input, marker, code string }{
		{"decimal", "<a>é\ntext&#0;</a>", "&#", "syntax"},
		{"hex", "<a>é\ntext&#x0;</a>", "&#", "syntax"},
		{"attribute", "<a x=\"é\ntext&#x0;\"/>", "&#", "syntax"},
		{"terminator", "<a>é\ntext]]></a>", "]]>", "syntax"},
		{"unterminated reference", "<a>é\ntext&bad", "&", "syntax"},
		{"long reference", "<a>é\ntext&" + strings.Repeat("a", 40), "&", "syntax"},
		{"truncated rune", "<a>é\ntext\xf0", "\xf0", "encoding"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode([]byte(tc.input), Options{})
			var e apierror.Error
			require.ErrorAs(t, err, &e)
			require.Equal(t, tc.code, e.Details().GetString("code", ""))
			require.EqualValues(t, strings.Index(tc.input, tc.marker), e.Details().GetInt("offset", 0))
			require.EqualValues(t, 2, e.Details().GetInt("line", 0))
			require.EqualValues(t, 5, e.Details().GetInt("column", 0))
		})
	}
}
