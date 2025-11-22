package aws

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		AWS(),
		S3(),
	}
}
