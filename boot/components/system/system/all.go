package system

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		Filesystem(),
		Environment(),
		Resources(),
		Interceptor(),
		Contracts(),
		Functions(),
		Process(),
	}
}
