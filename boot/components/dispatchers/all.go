package dispatchers

import "github.com/wippyai/runtime/api/boot"

// All returns all dispatcher boot components.
func All() []boot.Component {
	return []boot.Component{
		Clock(),
		Func(),
		Security(),
		HTTP(),
		WS(),
		Exec(),
		Stream(),
		Excel(),
	}
}
