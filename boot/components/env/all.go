// SPDX-License-Identifier: MPL-2.0

package env

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		Memory(),
		File(),
		OS(),
		Static(),
		Composite(),
		Variable(),
	}
}
