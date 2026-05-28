// SPDX-License-Identifier: MPL-2.0

package system

import (
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/components/dispatchers"
)

func All() []boot.Component {
	return []boot.Component{
		Cluster(),
		Topology(),
		Raft(),
		EventualReg(),
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
		PG(),
		dispatchers.PGDispatcher(),
	}
}
