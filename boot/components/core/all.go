package core

import (
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/components/dispatcher"
)

func All() []boot.Component {
	return []boot.Component{
		PIDGen(),
		Dispatcher(),
		dispatcher.Clock(),
		Profiler(),
		Registry(),
		Finder(),
		Security(),
		Supervisor(),
		Loader(),
		EventRouter(),
	}
}
