package core

import "github.com/wippyai/runtime/api/boot"

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
