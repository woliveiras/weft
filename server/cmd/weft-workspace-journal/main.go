package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/woliveiras/weft/server/internal/workspacejournal"
)

func main() {
	root := flag.String("root", "/var/lib/weft/workspaces", "workspace volume root")
	journal := flag.String("journal", "/var/lib/weft/journal/events.jsonl", "durable journal path")
	flag.Parse()
	writer, err := workspacejournal.OpenWriter(*journal)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = workspacejournal.Capture(ctx, *root, writer)
	if err := errors.Join(err, writer.Close()); err != nil {
		log.Fatal(err)
	}
}
