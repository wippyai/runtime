// SPDX-License-Identifier: MPL-2.0

package fs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSentinelErrors(t *testing.T) {
	t.Run("ErrClosed", func(t *testing.T) {
		assert.Equal(t, "filesystem is closed", ErrClosed.Error())
	})

	t.Run("ErrPermissionDenied", func(t *testing.T) {
		assert.Equal(t, "permission denied", ErrPermissionDenied.Error())
	})

	t.Run("ErrInvalidFileMode", func(t *testing.T) {
		assert.Equal(t, "invalid file mode: contains bits outside of fs.ModePerm", ErrInvalidFileMode.Error())
	})
}
