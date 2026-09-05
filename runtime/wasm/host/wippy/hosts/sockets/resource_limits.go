// SPDX-License-Identifier: MPL-2.0
package sockets

import (
	"errors"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func resourceLimitError(err error) *NetworkError {
	if errors.Is(err, preview2.ErrSocketLimit) {
		return &NetworkError{Code: NetworkErrorNewSocketLimit}
	}
	return &NetworkError{Code: NetworkErrorOutOfMemory}
}
