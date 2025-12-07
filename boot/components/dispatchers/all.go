package dispatchers

import "github.com/wippyai/runtime/api/boot"

// All returns all dispatcher boot components.
func All() []boot.Component {
	return []boot.Component{
		Clock(),
		CloudStorage(),
		Contract(),
		Func(),
		Security(),
		HTTP(),
		WS(),
		Exec(),
		Stream(),
		SQL(),
		Events(),
	}
}
