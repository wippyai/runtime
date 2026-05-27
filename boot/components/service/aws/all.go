// SPDX-License-Identifier: MPL-2.0

package aws

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		AWS(),
		S3(),
		SQS(),
	}
}
