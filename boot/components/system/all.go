package system

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		Cluster(),
		Topology(),
		Lifecycle(),
		Filesystem(),
		Environment(),
		Resources(),
		Factory(),
		ProcessManager(),
		Interceptor(),
		Contracts(),
		Functions(),
	}
}
