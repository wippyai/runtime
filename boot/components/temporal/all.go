package temporal

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		ClientInterceptor(),
		WorkerInterceptor(),
		DataConverter(),
		ClientManager(),
		WorkerManager(),
		ActivityListener(),
		WorkflowListener(),
		HostManager(),
	}
}
