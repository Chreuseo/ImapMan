package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/imapman/imapman/internal/api"
	"github.com/imapman/imapman/internal/config"
	"github.com/imapman/imapman/internal/processor"
	"github.com/imapman/imapman/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration error", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dataStore, err := store.Open(ctx, cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		logger.Error("database error", "error", err)
		os.Exit(1)
	}
	defer dataStore.DB.Close()
	go processor.Processor{Config: cfg, Store: dataStore, Logger: logger}.Run(ctx)
	server := &http.Server{Addr: cfg.Server.Address, Handler: api.Server{Store: dataStore, APISecret: cfg.APISecret}.Router(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	logger.Info("ImapMan started", "address", cfg.Server.Address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}
