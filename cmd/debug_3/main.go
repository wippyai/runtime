package main

import (
	"fmt"

	"github.com/pkg/errors"
)

func someOtherFunc() error {
	// Create error with stack trace
	return errors.New("original error")
}

func someFunc() error {
	// Wrap the error, adding context and keeping the stack trace
	return errors.Wrap(someOtherFunc(), "something went wrong")
}

func main() {
	err := someFunc()
	if err != nil {
		// Must use %+v to print the full stack trace
		fmt.Printf("%+v\n", err)
	}
}
