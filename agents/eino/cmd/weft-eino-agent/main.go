package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/woliveiras/weft/agents/eino/factory"
	"github.com/woliveiras/weft/agents/eino/protocol"
	"github.com/woliveiras/weft/agents/eino/summary"
)

func serve() error {
	agent, err := summary.New(context.Background())
	if err != nil {
		return fmt.Errorf("compile summarize: %w", err)
	}
	reviewer, err := factory.Reviewer(context.Background())
	if err != nil {
		return fmt.Errorf("compile reviewer: %w", err)
	}
	planner, err := factory.Planner(context.Background())
	if err != nil {
		return fmt.Errorf("compile planner: %w", err)
	}
	researcher, err := factory.Researcher(context.Background())
	if err != nil {
		return fmt.Errorf("compile researcher: %w", err)
	}
	handler, err := protocol.NewHandler(strings.Split(os.Getenv("WEFT_URL"), ","), os.Getenv("WEFT_RUNTIME_SECRET"), map[string]protocol.Agent{"summarize": agent, "reviewer": reviewer, "planner": planner, "researcher": researcher})
	if err != nil {
		return err
	}
	host := os.Getenv("HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	server := http.Server{Addr: net.JoinHostPort(host, port), Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: time.Minute}
	return server.ListenAndServe()
}

func main() {
	if err := serve(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
