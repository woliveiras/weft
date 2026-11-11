//go:build !linux

package workspacejournal

import (
	"context"
	"fmt"
)

func Capture(context.Context, string, *Writer) error {
	return fmt.Errorf("kernel workspace journal requires Linux fanotify inside the dedicated sandbox daemon")
}
