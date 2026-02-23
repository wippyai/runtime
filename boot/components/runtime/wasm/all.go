// SPDX-License-Identifier: MPL-2.0

package wasm

import "github.com/wippyai/runtime/api/boot"

// All returns all WASM runtime boot components.
func All() []boot.Component {
	return []boot.Component{
		Engine(),
	}
}
