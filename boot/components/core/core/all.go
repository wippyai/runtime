package core

import "github.com/ponyruntime/pony/api/boot"

func All() []boot.Component {
	return []boot.Component{
		PIDGen(),
		Profiler(),
		Registry(),
		Finder(),
		Security(),
		Supervisor(),
		Loader(),
		EventRouter(),
	}
}
