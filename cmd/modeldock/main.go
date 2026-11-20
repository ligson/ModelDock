package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/modeldock/modeldock/internal/api"
	"github.com/modeldock/modeldock/internal/db"
	"github.com/modeldock/modeldock/internal/download"
	"github.com/modeldock/modeldock/internal/runtime"
	"github.com/modeldock/modeldock/internal/source"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] != "serve" {
		if os.Args[1] == "--help" || os.Args[1] == "-h" {
			serveFlags("serve").Usage()
			return
		}
		fatalf("unknown command %q (use: modeldock serve)", os.Args[1])
	}

	args := os.Args[1:]
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	flags := serveFlags("serve")
	if err := flags.Parse(args); err != nil {
		fatalf("%v", err)
	}

	addr := flagValue(flags, "addr")
	root, err := absolutePath(defaultRoot)
	if err != nil {
		fatalf("invalid model root: %v", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		fatalf("create model root: %v", err)
	}
	dbPath, err := databasePath(defaultDB)
	if err != nil {
		fatalf("resolve database path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		fatalf("create database directory: %v", err)
	}

	store, err := db.Open(dbPath)
	if err != nil {
		fatalf("open database: %v", err)
	}
	defer store.Close()
	if configured := store.Setting(context.Background(), "storage.root", ""); configured != "" {
		root, err = absolutePath(configured)
		if err != nil {
			fatalf("invalid configured model root: %v", err)
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			fatalf("create configured model root: %v", err)
		}
	}

	if configured := runtime.ConfiguredRoot(); configured != "" {
		if err := os.MkdirAll(configured, 0o755); err != nil {
			fatalf("create runtime directory: %v", err)
		}
		if current := store.Setting(context.Background(), "runtime.root", ""); current == "" {
			_ = store.SetSetting(context.Background(), "runtime.root", configured)
		}
	}
	if err := store.SaveStorageRoot(context.Background(), db.StorageRoot{
		ID:        db.StorageRootID(root),
		Path:      root,
		Name:      filepath.Base(root),
		Default:   true,
		Available: true,
	}); err != nil {
		fatalf("save default model root: %v", err)
	}

	manager := &download.Manager{
		DB:     store,
		Root:   root,
		Client: source.NewHTTPClient(0),
		// A stalled proxy connection should be retried promptly; this is not a
		// whole-request timeout and does not limit slow but active downloads.
		StallTimeout: 30 * time.Second,
	}
	if err := manager.RecoverInterruptedTasks(context.Background()); err != nil {
		fatalf("recover download tasks: %v", err)
	}
	server := &api.Server{
		DB:            store,
		Downloads:     manager,
		Runtimes:      runtime.NewRegistry(),
		Generations:   make(map[string]*api.GenerationJob),
		Conversations: make(map[string][]api.ChatMessage),
	}
	if err := server.RestoreGenerationJobs(context.Background()); err != nil {
		fatalf("restore generation jobs: %v", err)
	}

	httpServer := &http.Server{Addr: addr, Handler: server.Handler()}
	go func() {
		fmt.Printf("ModelDock listening on http://%s\n", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fatalf("serve: %v", err)
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
	runtime.StopLlamaCPP()
}

func serveFlags(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: modeldock serve [options]\n")
		flags.PrintDefaults()
	}
	flags.String("addr", "127.0.0.1:8787", "HTTP listen address")
	flags.StringVar(&defaultRoot, "root", defaultModelRoot(), "model storage root")
	flags.StringVar(&defaultDB, "db", "", "SQLite database path (defaults to the user config directory)")
	return flags
}

func flagValue(flags *flag.FlagSet, name string) string {
	if value := flags.Lookup(name); value != nil {
		return value.Value.String()
	}
	return ""
}

var defaultRoot string
var defaultDB string

func absolutePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	return filepath.Abs(path)
}

func defaultModelRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "ModelDock"
	}
	return filepath.Join(home, "ModelDock")
}

func databasePath(path string) (string, error) {
	if path != "" {
		return absolutePath(path)
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(config, "ModelDock", "modeldock.db"), nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ModelDock: "+format+"\n", args...)
	os.Exit(1)
}
