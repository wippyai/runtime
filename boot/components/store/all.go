package store

import "github.com/wippyai/runtime/api/boot"

// All returns all store boot components.
func All() []boot.Component {
	return []boot.Component{
		Dispatcher(DefaultWorkers),
		MemStore(),
		SQLStore(),
	}
}
