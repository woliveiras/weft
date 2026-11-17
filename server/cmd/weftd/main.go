// Command weftd is the Weft control plane: API, engine, gateways and
// the web console in one binary. Run several for high availability; they
// coordinate through the shared store and broker.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/woliveiras/weft/server/internal/api"
	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/localinstall"
	"github.com/woliveiras/weft/server/internal/workspacejournal"
)

func main() {
	logFormat := flag.String("log-format", "", "log presentation: console, text or json (native default: console)")
	cfgPath := flag.String("config", "", "path to weft.yaml (or WEFT_CONFIG)")
	version := flag.Bool("version", false, "print version and exit")
	local := flag.Bool("local", false, "start an initialized native profile (no Docker required)")
	initLocal := flag.Bool("init-local", false, "initialize a new private native data directory")
	dataDir := flag.String("data-dir", "", "native data directory (default: OS user config directory/Weft)")
	webDir := flag.String("web-dir", "", "native init: bundled UI directory (default: web next to executable)")
	port := flag.Int("port", 8787, "native init: loopback console port")
	backupLocal := flag.String("backup-local", "", "write a private offline native profile archive to a new path")
	restoreLocal := flag.String("restore-local", "", "restore a trusted native backup to its original, absent data directory")
	pruneJournal := flag.Bool("prune-workspace-journal", false, "preview pruning fully imported workspace journal archives")
	applyPrune := flag.Bool("apply-journal-prune", false, "apply the workspace journal prune with the collector stopped")
	keepArchives := flag.Int("journal-keep-archives", workspacejournal.DefaultKeepArchives, "minimum number of newest workspace journal archives to retain when pruning")
	flag.Parse()
	if *pruneJournal && (*local || *initLocal || *backupLocal != "" || *restoreLocal != "" || *version) {
		fmt.Fprintln(os.Stderr, "--prune-workspace-journal cannot be combined with another action")
		os.Exit(2)
	}
	if *applyPrune && !*pruneJournal {
		fmt.Fprintln(os.Stderr, "--apply-journal-prune requires --prune-workspace-journal")
		os.Exit(2)
	}
	if *version {
		fmt.Println(api.Version)
		return
	}
	if *local || *initLocal || *backupLocal != "" || *restoreLocal != "" {
		actions := 0
		for _, yes := range []bool{*local, *initLocal, *backupLocal != "", *restoreLocal != ""} {
			if yes {
				actions++
			}
		}
		if *cfgPath != "" || actions != 1 {
			fmt.Fprintln(os.Stderr, "choose exactly one native action, without --config")
			os.Exit(2)
		}
		invalidStartFlag := false
		flag.Visit(func(f *flag.Flag) {
			if !*initLocal && (f.Name == "port" || f.Name == "web-dir") {
				invalidStartFlag = true
			}
		})
		if invalidStartFlag {
			fmt.Fprintln(os.Stderr, "--port and --web-dir apply only to --init-local; edit the native profile explicitly to change an existing installation")
			os.Exit(2)
		}
		root := *dataDir
		var err error
		if root == "" {
			root, err = localinstall.DefaultDir()
		} else {
			root, err = filepath.Abs(root)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if *backupLocal != "" || *restoreLocal != "" {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if *backupLocal != "" {
				err = localinstall.Backup(ctx, root, *backupLocal)
			} else {
				err = localinstall.Restore(ctx, *restoreLocal, root)
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "native backup/restore:", err)
				os.Exit(1)
			}
			fmt.Println("Native profile operation completed. Archives contain private credentials; protect them. Registered source repositories require separate backups.")
			return
		}
		if *initLocal {
			web := *webDir
			if web == "" {
				exe, e := os.Executable()
				if e != nil {
					fmt.Fprintln(os.Stderr, e)
					os.Exit(2)
				}
				web = filepath.Join(filepath.Dir(exe), "web")
			}
			web, err = filepath.Abs(web)
			if err == nil {
				err = localinstall.Init(root, web, *port)
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "native initialization:", err)
				os.Exit(1)
			}
			fmt.Println("Native profile initialized at", root)
			fmt.Println("Read admin.token in that directory yourself and enter it on the login screen. Keep it private.")
			fmt.Println("Start with: weftd --local --data-dir", root)
			return
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		format := *logFormat
		if format == "" {
			format = "console"
		}
		if err = localinstall.RunWithLogFormat(ctx, root, format); err != nil {
			fmt.Fprintln(os.Stderr, "native startup:", err)
			os.Exit(1)
		}
		return
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(2)
	}
	if *logFormat != "" {
		if *logFormat != "console" && *logFormat != "text" && *logFormat != "json" {
			fmt.Fprintln(os.Stderr, "log format must be console, text or json")
			os.Exit(2)
		}
		cfg.Observability.LogFormat = *logFormat
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *pruneJournal {
		if err := runJournalPrune(ctx, cfg, os.Getenv("WEFT_WORKSPACE_JOURNAL"), *keepArchives, *applyPrune, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "workspace journal prune:", err)
			os.Exit(1)
		}
		return
	}
	a, err := app.New(ctx, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "startup:", err)
		os.Exit(1)
	}
	if err := a.Run(ctx); err != nil {
		a.Log.Error("exited", "err", err)
		os.Exit(1)
	}
}
