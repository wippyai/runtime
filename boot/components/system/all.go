// SPDX-License-Identifier: MPL-2.0

package system

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		Cluster(),
		Topology(),
		Lifecycle(),
		Filesystem(),
		Environment(),
		Network(),
		SocketDispatcher(),
		Resources(),
		Factory(),
		ProcessManager(),
		Interceptor(),
		Contracts(),
		Functions(),
	}
}
