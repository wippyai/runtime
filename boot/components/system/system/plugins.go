package system

import "github.com/ponyruntime/pony/api/boot"

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
