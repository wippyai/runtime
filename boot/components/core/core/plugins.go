package core

import "github.com/ponyruntime/pony/api/boot"

func All() []boot.Component {
	return []boot.Component{
		Logger(),
		EventBus(),
		PIDGen(),
		LogManager(),
		Profiler(),
		Registry(),
		Security(),
		Supervisor(),
		Transcoder(),
		Loader(),
	}
}
