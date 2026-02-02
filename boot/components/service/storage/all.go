package storage

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		SQL(),
		TokenStore(),
	}
}
