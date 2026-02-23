// SPDX-License-Identifier: MPL-2.0

package core

import (
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/components/dispatchers"
)

func All() []boot.Component {
	components := []boot.Component{
		PIDGen(),
		Dispatcher(),
		Profiler(),
		Registry(),
		Finder(),
		Security(),
		SecurityPolicy(),
		Supervisor(),
		Loader(),
		EventRouter(),
	}
	return append(components, dispatchers.All()...)
}
