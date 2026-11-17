package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/egress"
)

func main() {
	hosts := flag.String("hosts", "", "comma-separated exact allowed DNS hosts")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	proxy, err := egress.New(strings.Split(*hosts, ","), log)
	if err != nil {
		log.Error("invalid egress policy")
		os.Exit(1)
	}
	server := &http.Server{Addr: ":3128", Handler: proxy, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 2 * time.Minute, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10}
	if err = server.ListenAndServe(); err != nil {
		log.Error("proxy stopped", "error", err)
		os.Exit(1)
	}
}
