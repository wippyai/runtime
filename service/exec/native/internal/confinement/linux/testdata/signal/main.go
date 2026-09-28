// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	fmt.Println(os.Getpid())
	for {
		time.Sleep(time.Second)
	}
}
