package queue

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		Manager(),
		Queues(),
		Memory(),
		Consumers(),
	}
}
