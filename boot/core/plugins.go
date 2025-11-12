package core

import "github.com/ponyruntime/pony/api/boot"

func All() []boot.Plugin {
	return []boot.Plugin{
		Logger(),
		EventBus(),
		PIDGen(),
		LogManager(),
		Profiler(),
		Registry(),
		Security(),
		Supervisor(),
		Transcoder(),
	}
}
