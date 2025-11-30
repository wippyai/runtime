package dispatcher

import "github.com/wippyai/runtime/api/boot"

// All returns all dispatcher boot components.
func All() []boot.Component {
	return []boot.Component{
		Clock(),
		Stream(),
		HTTP(),
		WS(),
		Func(),
		Store(),
		Queue(),
		Exec(),
		Excel(),
		Security(),
	}
}
