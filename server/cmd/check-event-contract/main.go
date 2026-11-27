// Command check-event-contract validates files exported by eventtest.Capture.
package main

import (
	"fmt"
	"os"

	"github.com/woliveiras/weft/server/internal/eventtest"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: check-event-contract CAPTURE_DIRECTORY")
		os.Exit(2)
	}
	events, kinds, scenarios, err := eventtest.ValidateCaptures(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Validated %d events of %d types from %d integration scenarios.\n", events, kinds, scenarios)
}
