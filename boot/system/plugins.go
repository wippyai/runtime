package system

import "github.com/ponyruntime/pony/api/boot"

func All() []boot.Plugin {
	return []boot.Plugin{
		Filesystem(),
		Environment(),
		Resources(),
		Interceptor(),
		Contracts(),
		Functions(),
		Process(),
	}
}
