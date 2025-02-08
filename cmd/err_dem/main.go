package main

import (
	"fmt"
	"github.com/pkg/errors"
)

func someFunction() error {
	err := errors.New("operation failed")
	if err != nil {
		return errors.Wrap(err, "operation failed")
	}
	return nil
}

// To get the stack trace from a wrapped error:
func handleError(err error) {
	// Method 1: Print full stack trace
	fmt.Printf("%+v\n", err) // %+v prints the error message and the stack trace

	// Method 2: Get stack trace programmatically
	if err, ok := err.(interface{ StackTrace() errors.StackTrace }); ok {
		frames := err.StackTrace()
		if len(frames) > 0 {
			// Use fmt.Sprintf with %v for file/line and %n for function
			fmt.Printf("%v", frames[0]) // prints file:line
			// Or with function name:
			// return fmt.Sprintf("%v %n", frames[0], frames[0])
		}
	}
}

// Usage example:
func main() {
	if err := someFunction(); err != nil {
		handleError(err)
	}
}
